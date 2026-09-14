// Package media 提供视频与图片的 HTTP 供流。
//
// 这是 Wails AssetServer 的自定义 Handler。Wails 在调用它之前会先尝试从嵌入的
// 前端资源里匹配 GET 请求，只有在资源里找不到（fs.ErrNotExist）时才会落到这里 ——
// 所以 /media/ 与 /art/ 天然会进来，而前端自己的文件不会。不需要（也不应该）
// 在这里做「回退到默认资源服务」的逻辑，那一步 Wails 已经做过了。
//
// Range 支持由 http.ServeContent 免费提供：206 响应、Content-Range、If-Range、
// If-Modified-Since、Accept-Ranges 全部现成。这正是当初 Python 侧
// preview_server.py 手写的那 60 行逻辑要做的事，在这里一行都不用写。
package media

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 路由前缀。两者目前处理方式相同（都是 ServeContent，内容类型由扩展名推断），
// 分开只是为了将来能对图片单独加缓存策略而不用改动视频路径。
const (
	VideoPrefix = "/media/"
	ArtPrefix   = "/art/"
)

// ErrPathEscape 表示请求路径试图逃出库根目录。
var ErrPathEscape = errors.New("路径逃出库根目录")

// ErrBadPath 表示请求路径不合法。
var ErrBadPath = errors.New("非法路径")

// LibraryResolver 把库标识解析为库根目录的绝对路径。
type LibraryResolver func(libraryID string) (root string, ok bool)

// Handler 提供 /media/ 与 /art/ 两个前缀下的文件。
type Handler struct {
	resolve LibraryResolver
}

// New 创建 Handler。
func New(resolve LibraryResolver) *Handler {
	return &Handler{resolve: resolve}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "仅支持 GET / HEAD", http.StatusMethodNotAllowed)
		return
	}

	rest, ok := trimPrefix(r.URL.Path)
	if !ok {
		// Wails 已经查过嵌入资源了，走到这里说明确实不存在。
		http.NotFound(w, r)
		return
	}

	libraryID, rel, found := strings.Cut(rest, "/")
	if !found || libraryID == "" || rel == "" {
		http.NotFound(w, r)
		return
	}

	root, ok := h.resolve(libraryID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	full, err := safeJoin(root, rel)
	if err != nil {
		http.Error(w, "非法路径", http.StatusForbidden)
		return
	}

	file, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func trimPrefix(urlPath string) (string, bool) {
	for _, prefix := range []string{VideoPrefix, ArtPrefix} {
		if rest, ok := strings.CutPrefix(urlPath, prefix); ok {
			return rest, true
		}
	}
	return "", false
}

// safeJoin 把相对路径拼到库根下，并确认结果没有逃出库根。
//
// 必须做这件事：路径直接来自前端请求，一个 ../ 就能读到磁盘上任意文件。
// 这里用两层防护 —— 先 path.Clean 掉上跳（"/"+rel 使结果始终落在根内），
// 再校验前缀。单靠任何一层都不够：Clean 只处理已识别的形式，
// 而前缀校验挡不住 Clean 之前就存在的语义歧义。
func safeJoin(root, rel string) (string, error) {
	if rel == "" || strings.ContainsRune(rel, 0) {
		return "", ErrBadPath
	}
	if filepath.IsAbs(rel) || path.IsAbs(rel) {
		return "", ErrBadPath
	}

	// 统一转成以 "/" 开头的规范形式，上跳会被 Clean 吃掉。
	cleaned := path.Clean("/" + rel)
	full := filepath.Join(root, filepath.FromSlash(cleaned))
	rootClean := filepath.Clean(root)

	if full != rootClean && !strings.HasPrefix(full, rootClean+string(filepath.Separator)) {
		return "", ErrPathEscape
	}
	return full, nil
}

// VideoURL 生成前端使用的视频地址。
//
// 媒体地址由 Go 生成并提供给前端，而不是让前端自己拼路径 —— 拼错的表现是
// 「封面永远是空白」，极难归因。
func VideoURL(libraryID, rel string) string {
	return VideoPrefix + libraryID + "/" + escapeRel(rel)
}

// ArtURL 生成封面或截图的地址。
func ArtURL(libraryID, rel string) string {
	return ArtPrefix + libraryID + "/" + escapeRel(rel)
}

// escapeRel 逐段转义相对路径。
//
// 不能对整个路径调 url.PathEscape：它会把分隔符一起转义掉，路由就无法再拆分
// 库标识与相对路径。而中文目录名必须转义，否则 WebView 发出的请求会被截断，
// 表现为「封面永远是空白」。
func escapeRel(rel string) string {
	segments := strings.Split(filepath.ToSlash(rel), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}
