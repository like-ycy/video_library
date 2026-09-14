package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SetFavorite 显式设置收藏状态。
func (s *Store) SetFavorite(
	ctx context.Context,
	libID, actress, fanha string,
	value bool,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_data (library_id, actress, fanha, favorite)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (library_id, actress, fanha)
		DO UPDATE SET favorite = excluded.favorite`,
		libID, actress, fanha, boolToInt(value))
	if err != nil {
		return fmt.Errorf("设置收藏: %w", err)
	}
	return nil
}

// ToggleFavorite 翻转收藏状态并返回翻转后的值。
//
// 用一条 upsert 完成「读-改-写」而不是先查后写：后者在两次操作之间可能被
// 别处的写入插入，得到与用户预期相反的结果。
func (s *Store) ToggleFavorite(
	ctx context.Context,
	libID, actress, fanha string,
) (bool, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_data (library_id, actress, fanha, favorite)
		VALUES (?, ?, ?, 1)
		ON CONFLICT (library_id, actress, fanha)
		DO UPDATE SET favorite = CASE WHEN user_data.favorite = 1 THEN 0 ELSE 1 END`,
		libID, actress, fanha)
	if err != nil {
		return false, fmt.Errorf("切换收藏: %w", err)
	}

	var favorite int
	err = s.db.QueryRowContext(ctx, `
		SELECT favorite FROM user_data
		 WHERE library_id = ? AND actress = ? AND fanha = ?`,
		libID, actress, fanha).Scan(&favorite)
	if err != nil {
		return false, fmt.Errorf("读取收藏状态: %w", err)
	}
	return favorite != 0, nil
}

// SetRating 设置评分。rating 为 nil 表示清除评分 —— 显式区分「未评分」和「0 分」，
// 否则用户打分后无法撤销。
func (s *Store) SetRating(
	ctx context.Context,
	libID, actress, fanha string,
	rating *int,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_data (library_id, actress, fanha, rating)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (library_id, actress, fanha)
		DO UPDATE SET rating = excluded.rating`,
		libID, actress, fanha, rating)
	if err != nil {
		return fmt.Errorf("设置评分: %w", err)
	}
	return nil
}

// SaveProgress 记录播放位置。
//
// 只在播放器上报进度时调用（播放中低频 + 关闭时一次），不要按秒写 ——
// 每个 tick 一次磁盘写入是没有意义的 IO。
func (s *Store) SaveProgress(
	ctx context.Context,
	libID, actress, fanha string,
	positionMs int64,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_data (library_id, actress, fanha, watch_position_ms)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (library_id, actress, fanha)
		DO UPDATE SET watch_position_ms = excluded.watch_position_ms`,
		libID, actress, fanha, positionMs)
	if err != nil {
		return fmt.Errorf("保存播放进度: %w", err)
	}
	return nil
}

// RecordPlay 累加播放次数并更新时间戳。
func (s *Store) RecordPlay(
	ctx context.Context,
	libID, actress, fanha string,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_data (library_id, actress, fanha, play_count, last_played_at)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT (library_id, actress, fanha)
		DO UPDATE SET play_count      = user_data.play_count + 1,
		              last_played_at  = excluded.last_played_at`,
		libID, actress, fanha, time.Now().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("记录播放: %w", err)
	}
	return nil
}

// UserData 是某个视频的用户数据。
type UserData struct {
	Favorite        bool   `json:"favorite"`
	Rating          *int   `json:"rating"`
	WatchPositionMs int64  `json:"watchPositionMs"`
	PlayCount       int64  `json:"playCount"`
	LastPlayedAt    string `json:"lastPlayedAt"`
}

// GetUserData 读取用户数据。从未操作过时返回零值而不是错误。
func (s *Store) GetUserData(
	ctx context.Context,
	libID, actress, fanha string,
) (UserData, error) {
	var (
		data       UserData
		favorite   int
		lastPlayed sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT favorite, rating, watch_position_ms, play_count, last_played_at
		  FROM user_data
		 WHERE library_id = ? AND actress = ? AND fanha = ?`,
		libID, actress, fanha).
		Scan(&favorite, &data.Rating, &data.WatchPositionMs, &data.PlayCount, &lastPlayed)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return UserData{}, nil
	case err != nil:
		return UserData{}, fmt.Errorf("读取用户数据: %w", err)
	}

	data.Favorite = favorite != 0
	data.LastPlayedAt = lastPlayed.String
	return data, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
