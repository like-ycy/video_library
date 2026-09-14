// Package probe 用 ffprobe 读取视频文件的真实媒体信息。
//
// 这些是站点元数据里拿不到的东西：站点只给「标注时长」（分钟，经常不准），
// 而文件真实时长、分辨率、编码只有读文件才知道。两者不能混用一个字段。
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrNotAvailable 表示没有可用的 ffprobe。
//
// 这是一条可降级路径：拿不到时长/编码不影响浏览与播放，
// 只是 UI 少几个字段，所以调用方应该记一笔然后继续，而不是让整次导入失败。
var ErrNotAvailable = errors.New("未找到可用的 ffprobe")

// MediaInfo 是 ffprobe 读出的媒体信息。
type MediaInfo struct {
	DurationMs int64
	Width      int
	Height     int
	VCodec     string
	ACodec     string
}

// Prober 串行化受限的 ffprobe 调用。
//
// 每次调用都是一个外部进程，全库导入时会是几千次进程创建。不限制并发会
// 让磁盘与 CPU 一起饱和，反而比串行更慢。
type Prober struct {
	path string
	sem  chan struct{}
}

// New 创建 Prober。path 为空时所有 Probe 调用返回 ErrNotAvailable。
func New(path string, concurrency int) *Prober {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Prober{path: path, sem: make(chan struct{}, concurrency)}
}

// Available 表示是否有可用的 ffprobe。
func (p *Prober) Available() bool {
	return p != nil && p.path != ""
}

// Probe 读取单个视频的媒体信息。
func (p *Prober) Probe(ctx context.Context, videoPath string) (MediaInfo, error) {
	if !p.Available() {
		return MediaInfo{}, ErrNotAvailable
	}

	p.sem <- struct{}{}
	defer func() { <-p.sem }()

	cmd := exec.CommandContext(ctx, p.path,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		videoPath,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return MediaInfo{}, fmt.Errorf("ffprobe %s: %s", filepath.Base(videoPath), detail)
	}
	return parse(stdout.Bytes())
}

// Locate 定位 ffprobe。
//
// 顺序：显式配置 → 分发目录 tools/bin → PATH。
// 随包分发的那份要优先于 PATH，否则结果会随用户装了哪个版本的 ffmpeg 而变。
func Locate(configured string) string {
	if configured != "" {
		return configured
	}
	name := "ffprobe"
	if runtime.GOOS == "windows" {
		name = "ffprobe.exe"
	}
	candidates := []string{
		filepath.Join("tools", "bin", name),
		filepath.Join("..", "tools", "bin", name),
	}
	for _, candidate := range candidates {
		if abs, err := filepath.Abs(candidate); err == nil {
			if _, statErr := exec.LookPath(abs); statErr == nil {
				return abs
			}
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	return ""
}

type ffprobeOutput struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

func parse(data []byte) (MediaInfo, error) {
	var raw ffprobeOutput
	if err := json.Unmarshal(data, &raw); err != nil {
		return MediaInfo{}, fmt.Errorf("解析 ffprobe 输出: %w", err)
	}

	info := MediaInfo{}
	// duration 是字符串形式的秒数。解析失败按「未知」处理，而不是报错：
	// 少数容器（尤其流式录制的 ts）本来就没有 duration。
	if seconds, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil && seconds > 0 {
		info.DurationMs = int64(seconds * 1000)
	}

	for _, stream := range raw.Streams {
		switch stream.CodecType {
		case "video":
			// 取第一条视频流：封面图等附属流通常排在后面。
			if info.VCodec == "" {
				info.VCodec = stream.CodecName
				info.Width = stream.Width
				info.Height = stream.Height
			}
		case "audio":
			if info.ACodec == "" {
				info.ACodec = stream.CodecName
			}
		}
	}
	return info, nil
}
