package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ReadSummary 读取演员汇总 JSON。
//
// 文件不存在时原样返回 os.ErrNotExist，由调用方解释为「尚未刮削」——
// 不要在这里返回空结构把两种状态压平：空结构既可能是「从没刮过」，
// 也可能是「刮过但一条都没成」，处理方式不同。
func ReadSummary(path string) (ActressSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ActressSummary{}, err
	}
	var summary ActressSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return ActressSummary{}, fmt.Errorf("解析 %s: %w", path, err)
	}
	return summary, nil
}

// MergeVideos 按番号把 updates 合并进边车 JSON 并原子写回，返回合并后的完整汇总。
//
// 必须是 merge，不能整体重建。既有实现每次刮削都重建整个 videos 数组，
// 结果是「本次没刮到的视频从 JSON 里消失」——一次网络故障或一次取消操作
// 就能静默删掉历史元数据，而且不会报任何错。
func MergeVideos(path, actress string, updates []Video) (ActressSummary, error) {
	summary := ActressSummary{Schema: SidecarSchema, Actress: actress}

	switch existing, err := ReadSummary(path); {
	case err == nil:
		summary = existing
	case errors.Is(err, os.ErrNotExist):
		// 首次刮削，用初始值。
	default:
		// JSON 已损坏。拒绝覆盖：直接重写会把里面或许还能救的数据一并抹掉，
		// 而且用户永远不知道发生了什么。
		return ActressSummary{}, err
	}

	summary.Schema = SidecarSchema
	summary.Actress = actress

	at := make(map[string]int, len(summary.Videos))
	for i, video := range summary.Videos {
		at[video.Fanha] = i
	}
	for _, update := range updates {
		if i, ok := at[update.Fanha]; ok {
			summary.Videos[i] = update
			continue
		}
		at[update.Fanha] = len(summary.Videos)
		summary.Videos = append(summary.Videos, update)
	}

	if err := writeSummaryAtomic(path, summary); err != nil {
		return ActressSummary{}, err
	}
	return summary, nil
}

// writeSummaryAtomic 先写临时文件再 rename。
//
// 就地覆盖写会在「写到一半断电」或「移动硬盘被拔」时留下半截 JSON，
// 之后这块目录的元数据全部解析失败 —— 一次写入事故毁掉整位演员的数据。
func writeSummaryAtomic(path string, summary ActressSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 meta 目录: %w", err)
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 %s: %w", path, err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入 %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换 %s: %w", path, err)
	}
	return nil
}
