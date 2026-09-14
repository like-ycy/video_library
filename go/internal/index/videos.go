package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound 表示目标记录不存在。
var ErrNotFound = errors.New("记录不存在")

// Row 是查询返回的一条视频记录，已把 videos 与 user_data 合并。
type Row struct {
	ID            int64    `json:"id"`
	LibraryID     string   `json:"libraryId"`
	Actress       string   `json:"actress"`
	Fanha         string   `json:"fanha"`
	Stem          string   `json:"stem"`
	Title         string   `json:"title"`
	ReleaseDate   string   `json:"releaseDate"`
	SiteLengthMin *int     `json:"siteLengthMin"`
	DurationMs    int64    `json:"durationMs"`
	Genres        []string `json:"genres"`
	Cast          []string `json:"cast"`
	CoverRel      string   `json:"coverRel"`
	ShotsRel      []string `json:"shotsRel"`
	VideoRel      string   `json:"videoRel"`
	FileSize      int64    `json:"fileSize"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	VCodec        string   `json:"vcodec"`
	ACodec        string   `json:"acodec"`
	Missing       bool     `json:"missing"`
	// Scraped 表示边车 JSON 里已有该番号的记录。
	//
	// 单独一列而不是让调用方去推断（比如「标题非空」或「scraped_at 非空」）：
	// 「是否已刮削」在扫描器和统计里必须是同一个定义，否则界面会出现
	// 「扫描说已刮削、统计说没刮削」这种自相矛盾的状态。
	Scraped   bool   `json:"scraped"`
	ScrapedAt string `json:"scrapedAt"`

	// 以下来自 user_data，即使从未操作过也会有零值。
	Favorite        bool  `json:"favorite"`
	Rating          *int  `json:"rating"`
	WatchPositionMs int64 `json:"watchPositionMs"`
	PlayCount       int64 `json:"playCount"`
}

// ActressCount 是演员维度的计数，用于侧栏。
type ActressCount struct {
	Actress string `json:"actress"`
	Total   int    `json:"total"`
	Scraped int    `json:"scraped"`
	Missing int    `json:"missing"`
}

// Filter 是查询条件。零值表示不限制。
type Filter struct {
	Actress      string
	Keyword      string   // 同时匹配番号与标题
	Genres       []string // AND 语义：必须同时包含全部所选类别
	FavoriteOnly bool
	// IncludeMissing 为 false（默认）时过滤掉文件已不在磁盘上的记录。
	IncludeMissing bool
	MinDurationMs  int64
}

// SortField 是允许的排序字段。
type SortField string

const (
	SortByReleaseDate SortField = "release_date"
	SortByFileSize    SortField = "file_size"
	SortByDuration    SortField = "duration_ms"
	SortByTitle       SortField = "title"
	SortByFanha       SortField = "fanha"
	SortByActress     SortField = "actress"
	SortByScrapedAt   SortField = "scraped_at"
)

// sortColumns 把排序字段映射到 SQL 列。
//
// 白名单而不是拼接用户输入：排序字段来自前端，直接拼进 SQL 就是注入面。
var sortColumns = map[SortField]string{
	SortByReleaseDate: "v.release_date",
	SortByFileSize:    "v.file_size",
	SortByDuration:    "v.duration_ms",
	SortByTitle:       "v.title",
	SortByFanha:       "v.fanha",
	SortByActress:     "v.actress",
	SortByScrapedAt:   "v.scraped_at",
}

// Sort 是排序要求。
type Sort struct {
	Field SortField
	Desc  bool
}

const rowColumns = `
  v.id, v.library_id, v.actress, v.fanha, v.stem, v.title, v.release_date,
  v.site_length_min, v.duration_ms, v.genres, v.cast_list, v.cover_rel,
  v.shots_rel, v.video_rel, v.file_size, v.width, v.height, v.vcodec, v.acodec,
  v.missing, v.scraped, v.scraped_at,
  COALESCE(ud.favorite, 0), ud.rating,
  COALESCE(ud.watch_position_ms, 0), COALESCE(ud.play_count, 0)`

// rowJoin 把用户数据并进来。
//
// 用业务键而不是 videos.id 关联：重建索引会重建 videos 的自增 id，
// 而 user_data 的主键是稳定的业务键，因此用户数据能在重建后幸存。
const rowJoin = `
  FROM videos v
  LEFT JOIN user_data ud
    ON ud.library_id = v.library_id
   AND ud.actress    = v.actress
   AND ud.fanha      = v.fanha`

// QueryVideos 按条件分页查询，返回记录与符合条件的总数。
func (s *Store) QueryVideos(
	ctx context.Context,
	libID string,
	filter Filter,
	sort Sort,
	limit, offset int,
) ([]Row, int, error) {
	where, args := buildWhere(libID, filter)
	clause := " WHERE " + strings.Join(where, " AND ")

	var total int
	countSQL := "SELECT COUNT(*)" + rowJoin + clause
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计记录数: %w", err)
	}

	column, ok := sortColumns[sort.Field]
	if !ok {
		column = sortColumns[SortByFanha]
	}
	direction := "ASC"
	if sort.Desc {
		direction = "DESC"
	}
	// 次级排序键固定，保证分页时顺序稳定 —— 否则同一批数据在翻页间可能重复
	// 或漏掉。
	order := fmt.Sprintf(" ORDER BY %s %s, v.fanha ASC", column, direction)

	if limit <= 0 {
		limit = 200
	}
	query := "SELECT" + rowColumns + rowJoin + clause + order + " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询视频: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []Row
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, row)
	}
	return result, total, rows.Err()
}

// GetVideo 按主键取一条记录（含用户数据）。
func (s *Store) GetVideo(ctx context.Context, libID string, id int64) (Row, error) {
	query := "SELECT" + rowColumns + rowJoin + " WHERE v.library_id = ? AND v.id = ?"
	row := s.db.QueryRowContext(ctx, query, libID, id)
	result, err := scanRow(row)
	if errors.Is(err, ErrNotFound) {
		return Row{}, err
	}
	return result, err
}

// ListActresses 返回演员维度的计数。
func (s *Store) ListActresses(ctx context.Context, libID string) ([]ActressCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT actress,
		       COUNT(*)                                     AS total,
		       SUM(scraped)                                 AS scraped,
		       SUM(missing)                                 AS missing
		  FROM videos
		 WHERE library_id = ?
		 GROUP BY actress
		 ORDER BY actress`, libID)
	if err != nil {
		return nil, fmt.Errorf("统计演员: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []ActressCount
	for rows.Next() {
		var item ActressCount
		if err := rows.Scan(&item.Actress, &item.Total, &item.Scraped, &item.Missing); err != nil {
			return nil, fmt.Errorf("解析演员统计: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// ListGenres 返回库中出现过的全部类别，用于筛选面板。
func (s *Store) ListGenres(ctx context.Context, libID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT json_each.value AS genre
		  FROM videos, json_each(videos.genres)
		 WHERE videos.library_id = ?
		 ORDER BY genre`, libID)
	if err != nil {
		return nil, fmt.Errorf("读取类别: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []string
	for rows.Next() {
		var genre string
		if err := rows.Scan(&genre); err != nil {
			return nil, fmt.Errorf("解析类别: %w", err)
		}
		result = append(result, genre)
	}
	return result, rows.Err()
}

// Stats 是库的整体统计。Vanished 单独列出，提醒用户"索引里有记录但文件不见了"。
type Stats struct {
	Total     int `json:"total"`
	Scraped   int `json:"scraped"`
	Unscraped int `json:"unscraped"`
	Missing   int `json:"missing"`
	Favorite  int `json:"favorite"`
}

// LibraryStats 返回库的整体统计。
func (s *Store) LibraryStats(ctx context.Context, libID string) (Stats, error) {
	var stats Stats
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       SUM(scraped),
		       SUM(CASE WHEN scraped = 0 THEN 1 ELSE 0 END),
		       SUM(missing)
		  FROM videos WHERE library_id = ?`, libID).
		Scan(&stats.Total, &stats.Scraped, &stats.Unscraped, &stats.Missing)
	if err != nil {
		return Stats{}, fmt.Errorf("统计库信息: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_data
		 WHERE library_id = ? AND favorite = 1`, libID).Scan(&stats.Favorite); err != nil {
		return Stats{}, fmt.Errorf("统计收藏: %w", err)
	}
	return stats, nil
}

func buildWhere(libID string, filter Filter) ([]string, []any) {
	where := []string{"v.library_id = ?"}
	args := []any{libID}

	if filter.Actress != "" {
		where = append(where, "v.actress = ?")
		args = append(args, filter.Actress)
	}
	if filter.Keyword != "" {
		pattern := "%" + escapeLike(filter.Keyword) + "%"
		where = append(where, `(v.fanha LIKE ? ESCAPE '\' OR v.title LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if filter.MinDurationMs > 0 {
		where = append(where, "v.duration_ms >= ?")
		args = append(args, filter.MinDurationMs)
	}
	if !filter.IncludeMissing {
		where = append(where, "v.missing = 0")
	}
	if filter.FavoriteOnly {
		where = append(where, "COALESCE(ud.favorite, 0) = 1")
	}
	// 类别用 json_each 而不是 LIKE '%"x"%'：后者在类别名互为子串时会误匹配，
	// 也会被名字里的引号破坏。
	for _, genre := range filter.Genres {
		where = append(where,
			"EXISTS (SELECT 1 FROM json_each(v.genres) WHERE json_each.value = ?)")
		args = append(args, genre)
	}
	return where, args
}

// escapeLike 转义 LIKE 的通配符。
//
// 不转义的话，搜索 "100%" 会匹配到全部记录 —— 用户看到的是「搜索坏了」，
// 而不是「我输了个特殊字符」。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// scanner 让 scanRow 同时接受 *sql.Row 与 *sql.Rows。
type scanner interface {
	Scan(dest ...any) error
}

// scanRow 按 rowColumns 的顺序解出一行。
//
// 三个 JSON 数组列（genres / cast_list / shots_rel）必须先扫成字符串再解码：
// 直接扫进 []string 不会被 database/sql 支持。
func scanRow(src scanner) (Row, error) {
	var (
		row     Row
		genres  string
		cast    string
		shots   string
		missing int
		scraped int
	)
	err := src.Scan(
		&row.ID, &row.LibraryID, &row.Actress, &row.Fanha, &row.Stem, &row.Title,
		&row.ReleaseDate, &row.SiteLengthMin, &row.DurationMs,
		&genres, &cast, &row.CoverRel, &shots, &row.VideoRel, &row.FileSize,
		&row.Width, &row.Height, &row.VCodec, &row.ACodec,
		&missing, &scraped, &row.ScrapedAt,
		&row.Favorite, &row.Rating, &row.WatchPositionMs, &row.PlayCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Row{}, ErrNotFound
	}
	if err != nil {
		return Row{}, fmt.Errorf("解析记录: %w", err)
	}

	row.Missing = missing != 0
	row.Scraped = scraped != 0
	row.Genres = decodeArray(genres)
	row.Cast = decodeArray(cast)
	row.ShotsRel = decodeArray(shots)
	return row, nil
}

func decodeArray(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		// 数据库里存着坏 JSON 时返回空切片而不是报错：一个字段的损坏不该让
		// 整个列表页打不开。
		return []string{}
	}
	return values
}
