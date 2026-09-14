package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// testDir 在模块根下建一个临时目录，测试结束即删。
//
// 不用 t.TempDir()：它建在系统临时目录下，在受限环境（沙箱、加固的 CI 容器）
// 里写入可能被拒绝，测试会以 error 收场而不是给出真实结论 —— 那比失败更糟，
// 因为它看起来像测试的问题而不是代码的问题。
func testDir(t *testing.T) string {
	t.Helper()
	base := filepath.Join("..", "..", ".gocache-test")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("创建测试目录 %s: %v", base, err)
	}
	dir, err := os.MkdirTemp(base, "case-")
	if err != nil {
		t.Fatalf("创建临时目录: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func sidecarPathFor(t *testing.T, root, actress string) string {
	t.Helper()
	return SidecarPath(ActressDir(root, actress), actress)
}

// TestMergeVideosKeepsEntriesNotInThisRun 钉住既有实现的数据丢失 bug。
//
// 旧实现每次刮削都整体重建 videos 数组，本次没刮到的条目会静默消失。
// 一次网络故障、一次取消操作就能删掉历史元数据，而且不报任何错。
func TestMergeVideosKeepsEntriesNotInThisRun(t *testing.T) {
	root := testDir(t)
	path := sidecarPathFor(t, root, "演员A")

	first := []Video{
		{Fanha: "ipzz-001", Stem: "IPZZ-001", Title: "第一部"},
		{Fanha: "ipzz-002", Stem: "IPZZ-002", Title: "第二部"},
	}
	if _, err := MergeVideos(path, "演员A", first); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}

	// 本次只刮到 002（001 失败了）。
	second := []Video{{Fanha: "ipzz-002", Stem: "IPZZ-002", Title: "第二部（已更新）"}}
	summary, err := MergeVideos(path, "演员A", second)
	if err != nil {
		t.Fatalf("合并写入失败: %v", err)
	}

	if len(summary.Videos) != 2 {
		t.Fatalf("合并后应有 2 条，实际 %d 条：001 被丢掉了", len(summary.Videos))
	}
	titles := map[string]string{}
	for _, video := range summary.Videos {
		titles[video.Fanha] = video.Title
	}
	if titles["ipzz-001"] != "第一部" {
		t.Errorf("未被本次涉及的条目应原样保留，实际 %q", titles["ipzz-001"])
	}
	if titles["ipzz-002"] != "第二部（已更新）" {
		t.Errorf("同番号条目应被覆盖，实际 %q", titles["ipzz-002"])
	}
}

func TestMergeVideosWritesSchemaAndActress(t *testing.T) {
	root := testDir(t)
	path := sidecarPathFor(t, root, "演员B")

	summary, err := MergeVideos(path, "演员B", []Video{{Fanha: "a-1"}})
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if summary.Schema != SidecarSchema {
		t.Errorf("schema = %d，期望 %d", summary.Schema, SidecarSchema)
	}
	if summary.Actress != "演员B" {
		t.Errorf("actress = %q", summary.Actress)
	}

	// 从磁盘读回来应当一致 —— 这是 Go 与外部工具之间的实际契约。
	reloaded, err := ReadSummary(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(reloaded.Videos) != 1 || reloaded.Videos[0].Fanha != "a-1" {
		t.Fatalf("读回内容不符：%+v", reloaded)
	}
}

func TestMergeVideosCreatesMissingDirectories(t *testing.T) {
	root := testDir(t)
	// meta 目录还不存在，MergeVideos 应当自己建出来。
	path := sidecarPathFor(t, root, "新演员")

	if _, err := MergeVideos(path, "新演员", []Video{{Fanha: "x-1"}}); err != nil {
		t.Fatalf("应自动创建 meta 目录，却失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("边车 JSON 未生成: %v", err)
	}
}

// TestMergeVideosRefusesToOverwriteCorruptJSON 保证损坏的文件不会被静默抹掉。
//
// 直接重写会把里面或许还能救的数据一并覆盖，而且用户永远不知道发生过什么。
func TestMergeVideosRefusesToOverwriteCorruptJSON(t *testing.T) {
	root := testDir(t)
	path := sidecarPathFor(t, root, "演员C")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	const broken = `{"actress": "演员C", "videos": [`
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatalf("写坏文件失败: %v", err)
	}

	if _, err := MergeVideos(path, "演员C", []Video{{Fanha: "a-1"}}); err == nil {
		t.Fatal("损坏的 JSON 应当报错，而不是被覆盖")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if string(after) != broken {
		t.Fatal("损坏的文件内容被改动了")
	}
}

// TestWriteIsAtomic 保证写入过程中不会留下半截文件。
//
// 就地覆盖写会在断电或移动硬盘被拔时留下不完整 JSON，之后整位演员的元数据
// 全部解析失败 —— 一次写入事故毁掉一整块目录的数据。
func TestWriteIsAtomic(t *testing.T) {
	root := testDir(t)
	path := sidecarPathFor(t, root, "演员D")

	if _, err := MergeVideos(path, "演员D", []Video{{Fanha: "a-1"}}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	// 临时文件必须已被 rename 掉，不能留在磁盘上。
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("临时文件应当已被清理，实际 stat 结果: %v", err)
	}
}

func TestReadSummaryReportsNotExist(t *testing.T) {
	root := testDir(t)
	_, err := ReadSummary(sidecarPathFor(t, root, "不存在"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("文件不存在时应返回 os.ErrNotExist，实际 %v", err)
	}
}

func TestScanMarksUnscrapedAndUnparsable(t *testing.T) {
	root := testDir(t)
	actressDir := ActressDir(root, "演员E")
	if err := os.MkdirAll(actressDir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	for _, name := range []string{"IPZZ-001.mp4", "命名不规范.mp4"} {
		if err := os.WriteFile(filepath.Join(actressDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("建文件失败: %v", err)
		}
	}
	if _, err := MergeVideos(SidecarPath(actressDir, "演员E"), "演员E",
		[]Video{{Fanha: "ipzz-001", Stem: "IPZZ-001", Cover: "meta/IPZZ-001/cover.jpg"}}); err != nil {
		t.Fatalf("写边车失败: %v", err)
	}

	result, err := Scan(root)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("应只识别出 1 个视频，实际 %d", len(result.Candidates))
	}

	candidate := result.Candidates[0]
	if candidate.Fanha != "ipzz-001" || candidate.Stem != "IPZZ-001" {
		t.Errorf("番号与 stem 都应保留：%+v", candidate)
	}
	if !candidate.Scraped {
		t.Error("边车 JSON 中已有该番号，Scraped 应为 true")
	}
	// 封面文件并不存在，因此应标为缺图而不是「已刮削」。
	if !candidate.MissingArt {
		t.Error("封面不存在时应标记 MissingArt")
	}
	// 相对库根的路径统一用正斜杠，换平台读取时必须仍然可用。
	if candidate.VideoRel != "演员E/IPZZ-001.mp4" {
		t.Errorf("VideoRel = %q", candidate.VideoRel)
	}

	if len(result.Issues) != 1 || result.Issues[0].Kind != IssueUnparsableName {
		t.Fatalf("命名不规范的视频应产生一条 Issue，实际 %+v", result.Issues)
	}
}
