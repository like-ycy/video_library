package index

import (
	"context"
	"fmt"
)

// ListContinue 返回有进度且尚未看完的视频。
//
// 「看完」定义为进度超过总时长的 97%，或进度超过 30 秒且文件已缺失时仍保留。
// 用户可能只是拖到片尾；阈值避免把片尾几秒当成未完成。
func (s *Store) ListContinue(ctx context.Context, libID string, limit int) ([]Row, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	query := "SELECT" + rowColumns + rowJoin + `
	  WHERE v.library_id = ?
	    AND ud.watch_position_ms > 30000
	    AND (v.duration_ms <= 0 OR ud.watch_position_ms < v.duration_ms * 97 / 100)
	    AND v.missing = 0
	  ORDER BY ud.last_played_at DESC, ud.watch_position_ms DESC
	  LIMIT ?`
	return s.queryRows(ctx, query, libID, limit)
}

// ListRecent 返回最近播放记录，按 last_played_at 倒序。
func (s *Store) ListRecent(ctx context.Context, libID string, limit int) ([]Row, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	query := "SELECT" + rowColumns + rowJoin + `
	  WHERE v.library_id = ?
	    AND COALESCE(ud.play_count, 0) > 0
	    AND ud.last_played_at IS NOT NULL
	    AND ud.last_played_at <> ''
	  ORDER BY ud.last_played_at DESC
	  LIMIT ?`
	return s.queryRows(ctx, query, libID, limit)
}

// ClearProgress 清除播放进度（不删播放次数）。
func (s *Store) ClearProgress(ctx context.Context, libID, actress, fanha string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_data (library_id, actress, fanha, watch_position_ms)
		VALUES (?, ?, ?, 0)
		ON CONFLICT (library_id, actress, fanha)
		DO UPDATE SET watch_position_ms = 0`,
		libID, actress, fanha)
	if err != nil {
		return fmt.Errorf("清除进度: %w", err)
	}
	return nil
}

// ClearRecent 清空该库的最近播放（进度与次数一并清零，保留收藏/评分）。
func (s *Store) ClearRecent(ctx context.Context, libID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE user_data
		   SET play_count = 0, last_played_at = '', watch_position_ms = 0
		 WHERE library_id = ?`,
		libID)
	if err != nil {
		return fmt.Errorf("清空最近播放: %w", err)
	}
	return nil
}
