package index

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"videolib/internal/library"
	"videolib/internal/probe"
)

// ImportStats 是一次导入的统计。
type ImportStats struct {
	// Found 是本次扫描到的视频条数。
	Found int
	// Vanished 是之前有记录、本次没扫到的条数（文件被删或被移走）。
	Vanished int
	// Probed 是本次实际跑了 ffprobe 的条数。
	Probed int
	// MediaCached 是复用了既有媒体信息、没有重复探测的条数。
	MediaCached int
	// Issues 是扫描中发现的非致命问题（文件名无法解析、边车 JSON 损坏等）。
	Issues []library.Issue
}

// Import 把某个视频库同步进索引，幂等，可随时重跑。
//
// 它负责协调 library（文件与边车 JSON）与 probe（媒体信息），但自身不实现其中
// 任何一方的逻辑。放在索引包而不是绑定层，是为了让 app.go 保持「只做参数校验
// 与事件转发」的薄度。
func Import(
	ctx context.Context,
	store *Store,
	libID string,
	root string,
	prober *probe.Prober,
	onProgress func(done, total int),
) (ImportStats, error) {
	scan, err := library.Scan(root)
	if err != nil {
		return ImportStats{}, err
	}
	stats := ImportStats{Found: len(scan.Candidates), Issues: scan.Issues}

	summaries, err := loadSummaries(scan.Candidates)
	if err != nil {
		return ImportStats{}, err
	}

	cached, err := store.mediaCache(ctx, libID)
	if err != nil {
		return ImportStats{}, err
	}

	pending, probed, reused, err := buildPending(ctx, scan.Candidates, summaries, cached, prober, onProgress)
	if err != nil {
		return ImportStats{}, err
	}
	stats.Probed = probed
	stats.MediaCached = reused

	if err := store.writePending(ctx, libID, root, pending); err != nil {
		return ImportStats{}, err
	}

	if stats.Vanished, err = store.countMissing(ctx, libID); err != nil {
		return ImportStats{}, err
	}
	return stats, nil
}

// pending 是一条待写入索引的完整记录。
type pending struct {
	candidate library.Candidate
	meta      library.Video
	info      probe.MediaInfo
}

// loadSummaries 按演员读取边车 JSON，每个演员只读一次。
//
// 一个演员目录下可能有几十个视频，逐个去读同一份 JSON 是无谓的重复 IO。
func loadSummaries(candidates []library.Candidate) (map[string]map[string]library.Video, error) {
	byActress := make(map[string]map[string]library.Video)

	for _, candidate := range candidates {
		if _, loaded := byActress[candidate.Actress]; loaded {
			continue
		}

		summary, err := library.ReadSummary(
			library.SidecarPath(candidate.ActressDir, candidate.Actress))
		if err != nil {
			// 缺失是正常状态（尚未刮削），损坏已经由 library.Scan 通过 Issues
			// 上报过一次。两种情况下这里都只能退化为「没有元数据」，
			// 索引里保留空字段，而不是让整次导入失败。
			byActress[candidate.Actress] = map[string]library.Video{}
			continue
		}

		videos := make(map[string]library.Video, len(summary.Videos))
		for _, video := range summary.Videos {
			videos[video.Fanha] = video
		}
		byActress[candidate.Actress] = videos
	}
	return byActress, nil
}

// buildPending 组装待写入记录，并决定哪些需要重新探测媒体信息。
func buildPending(
	ctx context.Context,
	candidates []library.Candidate,
	summaries map[string]map[string]library.Video,
	cached map[mediaKey]mediaState,
	prober *probe.Prober,
	onProgress func(done, total int),
) ([]pending, int, int, error) {
	items := make([]pending, 0, len(candidates))
	probed, reused := 0, 0
	available := prober.Available()

	for i, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, 0, 0, err
		}
		if onProgress != nil {
			onProgress(i, len(candidates))
		}

		item := pending{
			candidate: candidate,
			meta:      summaries[candidate.Actress][candidate.Fanha],
		}

		previous, hasPrevious := cached[mediaKey{actress: candidate.Actress, fanha: candidate.Fanha}]
		if hasPrevious {
			// 先继承既有媒体信息，探测成功时再覆盖。这样即使这次探测失败或
			// 被跳过，已有时长/编码也不会丢。
			item.info = previous.info
		}

		if !needsProbe(candidate, previous, hasPrevious, available) {
			reused++
			items = append(items, item)
			continue
		}

		info, err := prober.Probe(ctx, candidate.VideoPath)
		if err != nil {
			// 探测失败可降级：少几个展示字段，不影响浏览与播放。
			// 中断整次导入则会让一个损坏文件把整个库挡在门外。
			reused++
			items = append(items, item)
			continue
		}
		item.info = info
		probed++
		items = append(items, item)
	}

	if onProgress != nil {
		onProgress(len(candidates), len(candidates))
	}
	return items, probed, reused, nil
}

// needsProbe 判断是否需要为该视频重新读取媒体信息。
func needsProbe(
	candidate library.Candidate,
	previous mediaState,
	hasPrevious bool,
	proberAvailable bool,
) bool {
	if !proberAvailable {
		return false
	}
	if !hasPrevious {
		return true
	}
	// 文件改动过必须重探；上次没拿到时长也再试一次（可能是当时探测被中断）。
	return previous.fileMtime != candidate.FileMtime || previous.info.DurationMs == 0
}

// writePending 在单个事务里完成「标记消失 → 写入本次结果 → 更新库信息」。
//
// 必须在事务里：先全量置 missing=1 再逐条置 0。如果中途失败，整个库会被误标为
// 全部消失，用户看到的是「视频全没了」。
func (s *Store) writePending(
	ctx context.Context,
	libID string,
	root string,
	items []pending,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启导入事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 先假定全部消失，随后逐条置 0。
	// 这样不必构造巨大的 NOT IN 列表，代价是每条多一次 UPDATE 的写放大 ——
	// 几千条规模下可以忽略。
	if _, err := tx.ExecContext(ctx,
		`UPDATE videos SET missing = 1 WHERE library_id = ?`, libID); err != nil {
		return fmt.Errorf("标记待确认记录: %w", err)
	}

	statement, err := tx.PrepareContext(ctx, upsertVideoSQL)
	if err != nil {
		return fmt.Errorf("准备写入语句: %w", err)
	}
	defer func() { _ = statement.Close() }()

	now := time.Now().Format(time.RFC3339)
	for _, item := range items {
		if _, err := statement.ExecContext(ctx, upsertArgs(libID, item, now)...); err != nil {
			return fmt.Errorf("写入 %s/%s: %w", item.candidate.Actress, item.candidate.Fanha, err)
		}
	}

	if _, err := tx.ExecContext(ctx, upsertLibrarySQL, libID, root, now, len(items)); err != nil {
		return fmt.Errorf("更新库信息: %w", err)
	}
	return tx.Commit()
}

const upsertVideoSQL = `
INSERT INTO videos (
  library_id, actress, fanha, stem, title, release_date, site_length_min,
  duration_ms, genres, cast_list, cover_rel, shots_rel, video_rel, file_size,
  file_mtime, width, height, vcodec, acodec, missing, scraped, scraped_at, imported_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT (library_id, actress, fanha) DO UPDATE SET
  stem            = excluded.stem,
  title           = excluded.title,
  release_date    = excluded.release_date,
  site_length_min = excluded.site_length_min,
  genres          = excluded.genres,
  cast_list       = excluded.cast_list,
  cover_rel       = excluded.cover_rel,
  shots_rel       = excluded.shots_rel,
  video_rel       = excluded.video_rel,
  file_size       = excluded.file_size,
  file_mtime      = excluded.file_mtime,
  scraped_at      = excluded.scraped_at,
  imported_at     = excluded.imported_at,
  missing         = 0,
  scraped         = excluded.scraped,
  duration_ms     = CASE WHEN excluded.duration_ms > 0  THEN excluded.duration_ms ELSE videos.duration_ms END,
  width           = CASE WHEN excluded.width       > 0  THEN excluded.width       ELSE videos.width       END,
  height          = CASE WHEN excluded.height      > 0  THEN excluded.height      ELSE videos.height      END,
  vcodec          = CASE WHEN excluded.vcodec      <> '' THEN excluded.vcodec     ELSE videos.vcodec      END,
  acodec          = CASE WHEN excluded.acodec      <> '' THEN excluded.acodec     ELSE videos.acodec      END`

const upsertLibrarySQL = `
INSERT INTO libraries (id, root, last_scan_at, video_count) VALUES (?,?,?,?)
ON CONFLICT (id) DO UPDATE SET
  root         = excluded.root,
  last_scan_at = excluded.last_scan_at,
  video_count  = excluded.video_count`

func upsertArgs(libID string, item pending, now string) []any {
	c := item.candidate
	return []any{
		libID, c.Actress, c.Fanha, c.Stem,
		item.meta.Title, item.meta.ReleaseDate, item.meta.SiteLengthMin,
		item.info.DurationMs, jsonArray(item.meta.Genres), jsonArray(item.meta.Cast),
		item.meta.Cover, jsonArray(item.meta.Screenshots),
		c.VideoRel, c.FileSize, c.FileMtime,
		item.info.Width, item.info.Height, item.info.VCodec, item.info.ACodec,
		0, boolToInt(c.Scraped), item.meta.ScrapedAt, now,
	}
}

// ── 媒体信息缓存 ─────────────────────────────────────────────────────────

type mediaKey struct {
	actress string
	fanha   string
}

type mediaState struct {
	fileMtime int64
	info      probe.MediaInfo
}

func (s *Store) mediaCache(ctx context.Context, libID string) (map[mediaKey]mediaState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT actress, fanha, file_mtime, duration_ms, width, height, vcodec, acodec
		   FROM videos WHERE library_id = ?`, libID)
	if err != nil {
		return nil, fmt.Errorf("读取媒体信息缓存: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cache := make(map[mediaKey]mediaState)
	for rows.Next() {
		var (
			key  mediaKey
			item mediaState
		)
		if err := rows.Scan(&key.actress, &key.fanha, &item.fileMtime,
			&item.info.DurationMs, &item.info.Width, &item.info.Height,
			&item.info.VCodec, &item.info.ACodec); err != nil {
			return nil, fmt.Errorf("解析媒体信息缓存: %w", err)
		}
		cache[key] = item
	}
	return cache, rows.Err()
}

func (s *Store) countMissing(ctx context.Context, libID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM videos WHERE library_id = ? AND missing = 1`, libID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计消失记录: %w", err)
	}
	return count, nil
}

// jsonArray 把字符串切片编码为 JSON 数组。
//
// nil 切片会被 json.Marshal 编成 "null"，之后 json_each 查询会报错。
// 统一返回 "[]"，让「没有类别」在数据库里只有一种表示。
func jsonArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}
