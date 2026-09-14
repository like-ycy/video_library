package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"videolib/internal/library"
	"videolib/internal/probe"
)

const testLibID = "lib1"

func newTestEnv(t *testing.T) (context.Context, *Store, string) {
	t.Helper()

	base := filepath.Join("..", "..", ".gocache-test")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("创建测试目录: %v", err)
	}
	dir, err := os.MkdirTemp(base, "case-")
	if err != nil {
		t.Fatalf("创建临时目录: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := Open(filepath.Join(dir, "index.db"))
	if err != nil {
		t.Fatalf("打开索引库失败: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	root := filepath.Join(dir, "library")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("创建库根失败: %v", err)
	}
	return context.Background(), store, root
}

// seedVideo 在磁盘上造出一个完整的视频条目：视频文件、封面、边车 JSON。
func seedVideo(t *testing.T, root, actress, stem, fanha string, meta library.Video) string {
	t.Helper()

	actressDir := library.ActressDir(root, actress)
	artDir := library.ArtDir(actressDir, stem)
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	videoPath := filepath.Join(actressDir, stem+".mp4")
	if err := os.WriteFile(videoPath, []byte("fake video bytes"), 0o644); err != nil {
		t.Fatalf("创建视频文件失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artDir, "cover.jpg"), []byte("jpeg"), 0o644); err != nil {
		t.Fatalf("创建封面失败: %v", err)
	}

	meta.Fanha = fanha
	meta.Stem = stem
	meta.Cover = "meta/" + stem + "/cover.jpg"
	if meta.VideoFile == "" {
		meta.VideoFile = stem + ".mp4"
	}
	if _, err := library.MergeVideos(
		library.SidecarPath(actressDir, actress), actress, []library.Video{meta}); err != nil {
		t.Fatalf("写边车 JSON 失败: %v", err)
	}
	return videoPath
}

// noProber 返回一个不可用的 Prober，用于把导入流程与外部 ffprobe 解耦。
func noProber() *probe.Prober { return probe.New("", 1) }

func seedTwoVideos(t *testing.T, root string) {
	t.Helper()
	seedVideo(t, root, "演员A", "IPZZ-001", "ipzz-001", library.Video{
		Title: "标题一", ReleaseDate: "2024-03-15", Genres: []string{"类别A"}, Cast: []string{"演员甲"},
	})
	seedVideo(t, root, "演员A", "IPZZ-002", "ipzz-002", library.Video{
		Title: "标题二", ReleaseDate: "2024-05-01", Genres: []string{"类别A", "类别B"},
	})
}

// TestImportIsIdempotent 保证「重建索引」这个产品功能是可靠的。
func TestImportIsIdempotent(t *testing.T) {
	ctx, store, root := newTestEnv(t)
	seedTwoVideos(t, root)

	first, err := Import(ctx, store, testLibID, root, noProber(), nil)
	if err != nil {
		t.Fatalf("首次导入失败: %v", err)
	}
	if first.Found != 2 {
		t.Fatalf("Found = %d，期望 2", first.Found)
	}

	second, err := Import(ctx, store, testLibID, root, noProber(), nil)
	if err != nil {
		t.Fatalf("重复导入失败: %v", err)
	}
	if second.Found != 2 || second.Vanished != 0 {
		t.Fatalf("重复导入结果异常：%+v", second)
	}

	_, total, err := store.QueryVideos(ctx, testLibID, Filter{}, Sort{}, 100, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 2 {
		t.Fatalf("重复导入产生了重复行：total = %d", total)
	}
}

func TestImportReadsSidecarMetadata(t *testing.T) {
	ctx, store, root := newTestEnv(t)
	seedTwoVideos(t, root)
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	rows, _, err := store.QueryVideos(ctx, testLibID,
		Filter{Keyword: "IPZZ-001"}, Sort{}, 100, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应命中 1 条，实际 %d", len(rows))
	}

	row := rows[0]
	if row.Title != "标题一" {
		t.Errorf("Title = %q", row.Title)
	}
	if row.ReleaseDate != "2024-03-15" {
		t.Errorf("ReleaseDate = %q", row.ReleaseDate)
	}
	if len(row.Genres) != 1 || row.Genres[0] != "类别A" {
		t.Errorf("Genres = %v", row.Genres)
	}
	if row.VideoRel != "演员A/IPZZ-001.mp4" {
		t.Errorf("VideoRel = %q（必须是相对库根的正斜杠路径）", row.VideoRel)
	}
}

// TestQueryByGenreIsExactAndConjunctive 验证 json_each 过滤。
//
// 用 LIKE '%"类别A"%' 也能凑合，但类别名互为子串时会误匹配，
// 所以这里同时钉住「精确匹配」与「AND 语义」两件事。
func TestQueryByGenreIsExactAndConjunctive(t *testing.T) {
	ctx, store, root := newTestEnv(t)
	seedTwoVideos(t, root)
	// 一个类别名是另一个的子串，用来暴露 LIKE 方案的误匹配
	seedVideo(t, root, "演员B", "IPZZ-003", "ipzz-003", library.Video{
		Title: "标题三", Genres: []string{"类别"},
	})
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	cases := []struct {
		genres []string
		want   int
	}{
		{nil, 3},
		{[]string{"类别A"}, 2},
		{[]string{"类别B"}, 1},
		{[]string{"类别"}, 1},         // 不能命中「类别A」「类别B」
		{[]string{"类别A", "类别B"}, 1}, // AND 语义
		{[]string{"类别A", "不存在"}, 0},
	}

	for _, tc := range cases {
		_, total, err := store.QueryVideos(ctx, testLibID, Filter{Genres: tc.genres}, Sort{}, 100, 0)
		if err != nil {
			t.Fatalf("按类别 %v 查询失败: %v", tc.genres, err)
		}
		if total != tc.want {
			t.Errorf("类别 %v 命中 %d 条，期望 %d 条", tc.genres, total, tc.want)
		}
	}
}

// TestQueryKeywordEscapesWildcards 保证搜索框里的 % 不被当成通配符。
//
// 不转义时搜 "%" 会匹配到全部记录，用户看到的是「搜索坏了」，
// 而不是「我输了个特殊字符」。
func TestQueryKeywordEscapesWildcards(t *testing.T) {
	ctx, store, root := newTestEnv(t)
	seedTwoVideos(t, root)
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	for _, keyword := range []string{"%", "_", "标%题"} {
		_, total, err := store.QueryVideos(ctx, testLibID, Filter{Keyword: keyword}, Sort{}, 100, 0)
		if err != nil {
			t.Fatalf("搜索 %q 失败: %v", keyword, err)
		}
		if total != 0 {
			t.Errorf("搜索 %q 命中 %d 条，通配符未被转义", keyword, total)
		}
	}

	_, total, err := store.QueryVideos(ctx, testLibID, Filter{Keyword: "标题"}, Sort{}, 100, 0)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if total != 2 {
		t.Errorf("正常关键词应命中 2 条，实际 %d", total)
	}
}

// TestImportMarksVanishedWithoutDeleting 保证文件被移走时记录不会消失。
//
// 盘没插、文件临时移走都是常态。直接删记录意味着用户重新插上盘之后，
// 索引里空空如也，还得重新刮削。
func TestImportMarksVanishedWithoutDeleting(t *testing.T) {
	ctx, store, root := newTestEnv(t)
	videoPath := seedVideo(t, root, "演员A", "IPZZ-001", "ipzz-001", library.Video{Title: "标题一"})
	seedVideo(t, root, "演员A", "IPZZ-002", "ipzz-002", library.Video{Title: "标题二"})
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	if err := os.Remove(videoPath); err != nil {
		t.Fatalf("删除视频文件失败: %v", err)
	}
	stats, err := Import(ctx, store, testLibID, root, noProber(), nil)
	if err != nil {
		t.Fatalf("重新导入失败: %v", err)
	}
	if stats.Vanished != 1 {
		t.Fatalf("Vanished = %d，期望 1", stats.Vanished)
	}

	_, visible, err := store.QueryVideos(ctx, testLibID, Filter{}, Sort{}, 100, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if visible != 1 {
		t.Errorf("默认应隐藏缺失记录，实际可见 %d 条", visible)
	}

	_, all, err := store.QueryVideos(ctx, testLibID, Filter{IncludeMissing: true}, Sort{}, 100, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if all != 2 {
		t.Errorf("记录不应被删除，实际 %d 条", all)
	}
}

// TestReimportPreservesMediaInfoWhenNoProbe 钉住 upsert 里的 CASE WHEN。
//
// 探测是按文件 mtime 变化触发的，绝大多数导入不会有新的探测结果。
// 无条件覆盖会把已有的时长与编码清成 0，表现为「元数据莫名其妙消失了」。
func TestReimportPreservesMediaInfoWhenNoProbe(t *testing.T) {
	ctx, store, root := newTestEnv(t)
	seedTwoVideos(t, root)
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("首次导入失败: %v", err)
	}

	// 模拟上一次导入曾经探测成功。
	if _, err := store.db.Exec(
		`UPDATE videos SET duration_ms = 7200000, width = 1920, height = 1080, vcodec = 'h264'
		  WHERE library_id = ? AND fanha = 'ipzz-001'`, testLibID); err != nil {
		t.Fatalf("预置媒体信息失败: %v", err)
	}

	// 这次仍然没有可用的 ffprobe。
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("重新导入失败: %v", err)
	}

	var (
		duration int64
		width    int
		codec    string
	)
	err := store.db.QueryRow(
		`SELECT duration_ms, width, vcodec FROM videos WHERE library_id = ? AND fanha = 'ipzz-001'`,
		testLibID).Scan(&duration, &width, &codec)
	if err != nil {
		t.Fatalf("读取媒体信息失败: %v", err)
	}
	if duration != 7200000 || width != 1920 || codec != "h264" {
		t.Fatalf("无探测结果时不应清零已有媒体信息：duration=%d width=%d codec=%q",
			duration, width, codec)
	}
}

// TestUserDataSurvivesReimport 验证用户数据用业务键而非 videos.id 的价值。
func TestUserDataSurvivesReimport(t *testing.T) {
	ctx, store, root := newTestEnv(t)
	seedTwoVideos(t, root)
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	rows, _, err := store.QueryVideos(ctx, testLibID, Filter{Keyword: "IPZZ-001"}, Sort{}, 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("查询失败: err=%v rows=%d", err, len(rows))
	}
	target := rows[0]

	if _, err := store.ToggleFavorite(ctx, testLibID, target.Actress, target.Fanha); err != nil {
		t.Fatalf("收藏失败: %v", err)
	}
	if err := store.SaveProgress(ctx, testLibID, target.Actress, target.Fanha, 123456); err != nil {
		t.Fatalf("保存进度失败: %v", err)
	}

	// 重新导入：videos 行的自增 id 可能变化，但业务键不变。
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("重新导入失败: %v", err)
	}

	rows, _, err = store.QueryVideos(ctx, testLibID, Filter{Keyword: "IPZZ-001"}, Sort{}, 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("重新查询失败: err=%v rows=%d", err, len(rows))
	}
	if !rows[0].Favorite {
		t.Error("收藏状态在重新导入后丢失")
	}
	if rows[0].WatchPositionMs != 123456 {
		t.Errorf("播放进度在重新导入后丢失：%d", rows[0].WatchPositionMs)
	}
}

func TestRebuildIndexClearsData(t *testing.T) {
	ctx, store, root := newTestEnv(t)
	seedTwoVideos(t, root)
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	if err := store.Reset(ctx); err != nil {
		t.Fatalf("清空失败: %v", err)
	}

	if _, total, err := store.QueryVideos(ctx, testLibID, Filter{}, Sort{}, 100, 0); err != nil || total != 0 {
		t.Fatalf("清空后应无记录：err=%v total=%d", err, total)
	}
	// 清空之后必须还能重新导入 —— 重建索引是产品功能，不能一次性。
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("清空后重新导入失败: %v", err)
	}
	if _, total, err := store.QueryVideos(ctx, testLibID, Filter{}, Sort{}, 100, 0); err != nil || total != 2 {
		t.Fatalf("重新导入后应有 2 条：err=%v total=%d", err, total)
	}
}

func TestLibraryStatsAndActressCounts(t *testing.T) {
	ctx, store, root := newTestEnv(t)
	seedTwoVideos(t, root)
	seedVideo(t, root, "演员B", "SIRO-1000", "siro-1000", library.Video{Title: "标题三"})
	if _, err := Import(ctx, store, testLibID, root, noProber(), nil); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	stats, err := store.LibraryStats(ctx, testLibID)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats.Total != 3 || stats.Scraped != 3 || stats.Unscraped != 0 {
		t.Fatalf("统计不符：%+v", stats)
	}

	actresses, err := store.ListActresses(ctx, testLibID)
	if err != nil {
		t.Fatalf("读取演员失败: %v", err)
	}
	if len(actresses) != 2 {
		t.Fatalf("应有 2 位演员，实际 %d", len(actresses))
	}
	if actresses[0].Actress != "演员A" || actresses[0].Total != 2 {
		t.Fatalf("演员统计不符：%+v", actresses[0])
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	base := filepath.Join("..", "..", ".gocache-test")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("创建测试目录: %v", err)
	}
	dir, err := os.MkdirTemp(base, "case-")
	if err != nil {
		t.Fatalf("创建临时目录: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "index.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("首次打开失败: %v", err)
	}
	// 模拟「数据库来自更新版本的程序」。
	if _, err := store.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("写入版本失败: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	// 降级运行会把新版本写入的数据弄坏，必须拒绝而不是尽力而为。
	if _, err := Open(path); err == nil {
		t.Fatal("打开更高版本的数据库应当报错")
	}
}
