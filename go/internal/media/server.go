package media

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// StreamServer 在 127.0.0.1 上用真正的 net/http 供视频流。
//
// 为什么不能只靠 Wails AssetServer：Windows 的 WebView2 响应写入器会把整个
// 响应体缓冲进内存（bytes.Buffer → SHCreateMemStream → PutByteContent），
// 几 GB 的视频会把进程内存打满，表现为「一点内嵌播放软件就退出」。
// net/http 的 ServeContent 边读边写，Range 由标准库处理，不受该限制。
//
// 封面 / 截图仍走 Wails 的 /art/：体积小，缓冲无感，且不值得为此拆第二个源。
type StreamServer struct {
	server *http.Server
	base   string
}

// StartStreamServer 启动本地流媒体服务。端口随机，只绑 127.0.0.1。
func StartStreamServer(resolve LibraryResolver) (*StreamServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("监听视频流端口: %w", err)
	}

	s := &StreamServer{
		server: &http.Server{
			Handler:           New(resolve),
			ReadHeaderTimeout: 10 * time.Second,
		},
		base: "http://" + ln.Addr().String(),
	}
	go func() {
		// Serve 在 Close 后返回 http.ErrServerClosed，不是故障。
		_ = s.server.Serve(ln)
	}()
	return s, nil
}

// Base 返回服务根地址，例如 http://127.0.0.1:54321。
func (s *StreamServer) Base() string { return s.base }

// VideoURL 返回可直接作为 <video src> 的绝对地址。
func (s *StreamServer) VideoURL(libraryID, rel string) string {
	return s.base + VideoURL(libraryID, rel)
}

// Close 停止服务。幂等，可在 shutdown 与 Close 中各调一次。
func (s *StreamServer) Close() error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Close()
}
