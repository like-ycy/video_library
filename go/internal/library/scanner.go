package library

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IssueKind 标示扫描中发现的问题类别。
type IssueKind string

const (
	// IssueUnparsableName 文件名提取不出番号。不中断扫描，但要列给用户看。
	IssueUnparsableName IssueKind = "unparsable_name"
	// IssueSidecar 边车 JSON 读取或解析失败。
	IssueSidecar IssueKind = "sidecar"
)

// Issue 是扫描中发现的一个非致命问题。
//
// 之所以收集而不是直接返回错误：一个命名不规范的文件或一个损坏的 JSON
// 不应该让整次扫描失败 —— 用户会因此看不到其它几百条本来正常的视频。
type Issue struct {
	Kind    IssueKind
	Actress string
	Subject string // 触发问题的文件名或路径
	Message string
}

// ScanResult 是一次扫描的产出。
type ScanResult struct {
	Candidates []Candidate
	Issues     []Issue
}

// Scan 遍历库根下的演员目录，收集视频并标注刮削状态。
func Scan(root string) (ScanResult, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ScanResult{}, fmt.Errorf("读取库根目录 %s: %w", root, err)
	}

	var result ScanResult
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == MetaDirName {
			continue
		}
		candidates, issues := scanActress(root, entry.Name())
		result.Candidates = append(result.Candidates, candidates...)
		result.Issues = append(result.Issues, issues...)
	}

	sort.Slice(result.Candidates, func(i, j int) bool {
		a, b := result.Candidates[i], result.Candidates[j]
		if a.Actress != b.Actress {
			return a.Actress < b.Actress
		}
		// 排序稳定，UI 每次刷新顺序才不会跳。用番号而不是文件名做次序键：
		// 文件名带修饰后缀（-c / -4k）时，两者的顺序不一致会造成困惑。
		return a.Fanha < b.Fanha
	})
	return result, nil
}

func scanActress(root, actress string) ([]Candidate, []Issue) {
	actressDir := ActressDir(root, actress)

	summary, issue := loadSummary(actressDir, actress)
	var issues []Issue
	if issue != nil {
		issues = append(issues, *issue)
	}

	known := make(map[string]Video, len(summary.Videos))
	for _, video := range summary.Videos {
		known[video.Fanha] = video
	}

	entries, err := os.ReadDir(actressDir)
	if err != nil {
		return nil, append(issues, Issue{
			Kind: IssueSidecar, Actress: actress, Subject: actressDir,
			Message: fmt.Sprintf("读取演员目录失败: %v", err),
		})
	}

	var candidates []Candidate
	for _, entry := range entries {
		if entry.IsDir() || !IsVideoFile(entry.Name()) {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		fanha, ok := NormalizeFanha(stem)
		if !ok {
			issues = append(issues, Issue{
				Kind: IssueUnparsableName, Actress: actress, Subject: entry.Name(),
				Message: "无法从文件名提取番号，重命名后重新扫描",
			})
			continue
		}

		var size, mtime int64
		if info, statErr := entry.Info(); statErr == nil {
			size = info.Size()
			mtime = info.ModTime().Unix()
		}

		videoPath := filepath.Join(actressDir, entry.Name())
		record, scraped := known[fanha]
		candidate := Candidate{
			Actress:    actress,
			Fanha:      fanha,
			Stem:       stem,
			ActressDir: actressDir,
			VideoPath:  videoPath,
			VideoRel:   relToRoot(root, videoPath),
			VideoFile:  entry.Name(),
			FileSize:   size,
			FileMtime:  mtime,
			Scraped:    scraped,
		}
		if scraped {
			candidate.MissingArt = !artExists(actressDir, record.Cover)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, issues
}

// loadSummary 读取边车 JSON。文件不存在是正常状态（尚未刮削），不产生 Issue。
func loadSummary(actressDir, actress string) (ActressSummary, *Issue) {
	path := SidecarPath(actressDir, actress)
	summary, err := ReadSummary(path)
	if err == nil {
		return summary, nil
	}
	if os.IsNotExist(err) {
		return ActressSummary{Schema: SidecarSchema, Actress: actress}, nil
	}
	return ActressSummary{Schema: SidecarSchema, Actress: actress}, &Issue{
		Kind: IssueSidecar, Actress: actress, Subject: path, Message: err.Error(),
	}
}

// artExists 检查封面文件是否真的在磁盘上且非空。
//
// 只看 JSON 里的字段是不够的：写入字段与实际下载图片是两步，
// 上次刮削中断会留下「有记录、没图片」的状态。
func artExists(actressDir, coverRel string) bool {
	if coverRel == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(actressDir, filepath.FromSlash(coverRel)))
	return err == nil && !info.IsDir() && info.Size() > 0
}

// relToRoot 返回相对库根的路径，统一用正斜杠 —— 这个值会进数据库，
// 换平台读取时必须仍然可用。数据库中永不存绝对路径。
func relToRoot(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}
