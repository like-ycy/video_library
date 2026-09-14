package media

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestSafeJoinNeverEscapesRoot 是安全不变量测试。
//
// 路径直接来自前端请求，一个 ../ 就能读到磁盘上任意文件。这里不要求每种写法
// 都必须报错（Clean 会把上跳吃掉），但要求任何返回值都落在库根之内。
func TestSafeJoinNeverEscapesRoot(t *testing.T) {
	root := filepath.FromSlash("/library")
	cases := []string{
		"../secret.txt",
		"../../etc/passwd",
		"a/../../etc/passwd",
		"....//....//etc/passwd",
		"./a/./b",
		"演员A/meta/IPZZ-001/cover.jpg",
		"deeply/nested/path/file.mp4",
	}

	for _, rel := range cases {
		full, err := safeJoin(root, rel)
		if err != nil {
			continue // 明确拒绝也是正确结果
		}
		if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
			t.Errorf("safeJoin(%q) = %q，逃出了库根", rel, full)
		}
	}
}

func TestSafeJoinRejectsAbsoluteEmptyAndNul(t *testing.T) {
	root := filepath.FromSlash("/library")
	for _, rel := range []string{"", "/etc/passwd", "a\x00b"} {
		if _, err := safeJoin(root, rel); err == nil {
			t.Errorf("safeJoin(root, %q) 应当被拒绝", rel)
		}
	}
}

func TestSafeJoinKeepsNormalPaths(t *testing.T) {
	root := filepath.FromSlash("/library")
	full, err := safeJoin(root, "演员A/meta/IPZZ-001/cover.jpg")
	if err != nil {
		t.Fatalf("正常路径被拒绝: %v", err)
	}
	want := filepath.Join(root, "演员A", "meta", "IPZZ-001", "cover.jpg")
	if full != want {
		t.Fatalf("safeJoin = %q，期望 %q", full, want)
	}
}

func newTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	root := testDir(t)

	actressDir := filepath.Join(root, "演员A")
	if err := os.MkdirAll(actressDir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(actressDir, "a.mp4"), []byte("0123456789abcdef"), 0o644); err != nil {
		t.Fatalf("建文件失败: %v", err)
	}

	return New(func(id string) (string, bool) {
		if id == "lib1" {
			return root, true
		}
		return "", false
	}), root
}

func TestHandlerServesFile(t *testing.T) {
	handler, _ := newTestHandler(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/lib1/演员A/a.mp4", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if rec.Body.String() != "0123456789abcdef" {
		t.Fatalf("响应体不符：%q", rec.Body.String())
	}
}

// TestHandlerSupportsRange 保证视频可以拖动进度条。
//
// 这条能力由 http.ServeContent 免费提供，替代了原先 Python 侧手写的 60 行
// Range 逻辑。测试存在的意义是：如果将来有人把它换成 os.ReadFile + Write，
// 视频拖动会静默失效。
func TestHandlerSupportsRange(t *testing.T) {
	handler, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/media/lib1/演员A/a.mp4", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("状态码 = %d，期望 206", rec.Code)
	}
	if rec.Body.String() != "2345" {
		t.Fatalf("分段内容不符：%q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 2-5/16" {
		t.Fatalf("Content-Range = %q", got)
	}
}

func TestHandlerRejectsNonGetMethods(t *testing.T) {
	handler, _ := newTestHandler(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/media/lib1/演员A/a.mp4", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d，期望 405", rec.Code)
	}
}

func TestHandlerReturns404ForUnknownRoutes(t *testing.T) {
	handler, _ := newTestHandler(t)

	cases := []string{
		"/index.html",          // 前端资源不会被路由到这里，但真到了也不该处理
		"/media/unknown/a.mp4", // 未注册的库
		"/media/lib1",          // 缺少相对路径
		"/media/lib1/不存在.mp4",  // 文件不存在
		"/art/lib1/演员A/无图.jpg", // 图片分支同理
	}

	for _, target := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s 的状态码 = %d，期望 404", target, rec.Code)
		}
	}
}

func TestEscapeRelKeepsSeparators(t *testing.T) {
	// 分隔符必须保留，否则路由无法拆分库标识与相对路径；
	// 中文必须转义，否则 WebView 发出的请求会被截断。
	got := escapeRel("演员A/meta/封面 1.jpg")
	if !strings.Contains(got, "/") {
		t.Fatalf("分隔符被转义掉了：%q", got)
	}
	if got == "演员A/meta/封面 1.jpg" {
		t.Fatalf("非 ASCII 与空格应当被转义：%q", got)
	}
	if !strings.HasPrefix(VideoURL("lib1", "a/b.mp4"), "/media/lib1/") {
		t.Fatal("VideoURL 前缀不符")
	}
	if !strings.HasPrefix(ArtURL("lib1", "a/b.jpg"), "/art/lib1/") {
		t.Fatal("ArtURL 前缀不符")
	}
}
