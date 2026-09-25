package library

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
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

// scanOne 建一个只含一条视频的库（record 写在边车里），返回该候选与演员目录。
//
// artFiles 是额外要落盘的图片（相对演员目录）—— 用来构造「哪些图在、哪些不在」
// 的具体形态。传 nil 就是「边车有记录、磁盘上一张图也没有」。
func scanOne(t *testing.T, actress string, record Video, artFiles ...string) (Candidate, string) {
	t.Helper()

	root := testDir(t)
	actressDir := ActressDir(root, actress)
	if err := os.MkdirAll(actressDir, 0o755); err != nil {
		t.Fatalf("建演员目录失败: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(actressDir, "IPZZ-001.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatalf("建视频文件失败: %v", err)
	}
	for _, rel := range artFiles {
		path := filepath.Join(actressDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("建图片目录失败: %v", err)
		}
		if err := os.WriteFile(path, []byte("jpg"), 0o644); err != nil {
			t.Fatalf("建图片失败: %v", err)
		}
	}
	if _, err := MergeVideos(
		SidecarPath(actressDir, actress), actress, []Video{record}); err != nil {
		t.Fatalf("写边车失败: %v", err)
	}

	result, err := Scan(root)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("应识别出 1 个视频，实际 %d", len(result.Candidates))
	}
	return result.Candidates[0], actressDir
}

// TestScanFlagsMissingScreenshots 钉住「有 json、没有 images」这种形态。
//
// 旧实现只看封面：封面还在、截图整个目录丢了时，UI 仍显示「已刮削」，
// 而用户在磁盘上看到的是「只有 json 和一张封面」——两边说的不是一件事。
func TestScanFlagsMissingScreenshots(t *testing.T) {
	art := "meta/IPZZ-001"
	record := Video{
		Fanha: "ipzz-001", Stem: "IPZZ-001",
		Cover: art + "/cover.jpg",
		Screenshots: []string{
			art + "/images/1.jpg",
			art + "/images/2.jpg",
		},
	}
	// 只把封面建出来，截图一张都不建。
	candidate, actressDir := scanOne(t, "演员F", record, art+"/cover.jpg")

	if !candidate.MissingArt {
		t.Error("截图缺失也应算缺图，否则界面上看不出问题")
	}
	want := []string{art + "/images/1.jpg", art + "/images/2.jpg"}
	if !slices.Equal(candidate.MissingFiles, want) {
		t.Errorf("MissingFiles = %v，期望 %v", candidate.MissingFiles, want)
	}
	if candidate.ArtDir != ArtDir(actressDir, "IPZZ-001") {
		t.Errorf("ArtDir = %q，期望图片目录的绝对路径", candidate.ArtDir)
	}
}

// TestScanFlagsRecordWithoutArtFields 钉住「记录里压根没有图片字段」。
//
// 边车来自别的工具（或从没刮到过图）时，一个可比较的路径都没有。这时不能因为
// 「没有文件缺失」就判成齐全 —— 那是「缺图」最容易被漏掉的一种形态。
func TestScanFlagsRecordWithoutArtFields(t *testing.T) {
	candidate, _ := scanOne(t, "演员G", Video{Fanha: "ipzz-001", Stem: "IPZZ-001"})

	if !candidate.MissingArt {
		t.Error("记录里没有任何图片字段，应算缺图")
	}
	if len(candidate.MissingFiles) != 0 {
		t.Errorf("没有记录就没有可列的缺失文件，实际 %v", candidate.MissingFiles)
	}
}

// TestScanAcceptsCompleteArt 确认加固之后没有把正常的条目误判成缺图。
func TestScanAcceptsCompleteArt(t *testing.T) {
	art := "meta/IPZZ-001"
	record := Video{
		Fanha: "ipzz-001", Stem: "IPZZ-001",
		Cover:       art + "/cover.jpg",
		Screenshots: []string{art + "/images/1.jpg", art + "/images/2.jpg"},
	}
	candidate, _ := scanOne(t, "演员H", record,
		art+"/cover.jpg", art+"/images/1.jpg", art+"/images/2.jpg")

	if candidate.MissingArt {
		t.Errorf("图片齐全时不该报缺图，MissingFiles = %v", candidate.MissingFiles)
	}
}

func TestJobIDIsUniquePerFile(t *testing.T) {
	// 同一番号的两个文件必须有不同标识 —— 否则事件又会互相覆盖。
	a := JobID("演员A", "IPZZ-001")
	b := JobID("演员A", "IPZZ-001-c")
	if a == b {
		t.Fatalf("同番号的不同文件必须有不同的 job 标识：%q", a)
	}
	if a != "演员A/IPZZ-001" {
		t.Errorf("JobID = %q，期望 演员A/IPZZ-001", a)
	}
}
