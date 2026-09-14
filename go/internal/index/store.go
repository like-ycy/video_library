// Package index 维护 SQLite 派生索引。
//
// 定位：索引不是真相，真相是文件系统。数据库可以随时删掉重建，代价是几秒钟的
// 扫描，而不是数据丢失。因此这里从不保存绝对路径，也不假设自己是唯一的数据
// 来源 —— 任何一条记录都能从磁盘上的文件与边车 JSON 重新推导出来。
//
// 唯一不满足这个性质的是 user_data（收藏、播放进度），所以它用业务键
// (library_id, actress, fanha) 而不是 videos.id 做主键：重建索引时 videos 的自增
// id 会全部改变，业务键则保持稳定，用户数据因此能幸存。
package index

import (
	"context"
	"database/sql"
	"fmt"

	// 纯 Go 的 SQLite 实现，无 CGO。
	// 不要换成 mattn/go-sqlite3：它需要 CGO，会让 Wails 交叉编译变成噩梦。
	_ "modernc.org/sqlite"
)

// schemaVersion 与 schemaSQL 必须同步更新。
const schemaVersion = 1

const schemaSQL = `
CREATE TABLE IF NOT EXISTS videos (
  id               INTEGER PRIMARY KEY,
  library_id       TEXT    NOT NULL,
  actress          TEXT    NOT NULL,
  fanha            TEXT    NOT NULL,
  stem             TEXT    NOT NULL,
  title            TEXT    NOT NULL DEFAULT '',
  release_date     TEXT    NOT NULL DEFAULT '',
  site_length_min  INTEGER,
  duration_ms      INTEGER NOT NULL DEFAULT 0,
  genres           TEXT    NOT NULL DEFAULT '[]',
  cast_list        TEXT    NOT NULL DEFAULT '[]',
  cover_rel        TEXT    NOT NULL DEFAULT '',
  shots_rel        TEXT    NOT NULL DEFAULT '[]',
  video_rel        TEXT    NOT NULL,
  file_size        INTEGER NOT NULL DEFAULT 0,
  file_mtime       INTEGER NOT NULL DEFAULT 0,
  width            INTEGER NOT NULL DEFAULT 0,
  height           INTEGER NOT NULL DEFAULT 0,
  vcodec           TEXT    NOT NULL DEFAULT '',
  acodec           TEXT    NOT NULL DEFAULT '',
  missing          INTEGER NOT NULL DEFAULT 0,
  scraped          INTEGER NOT NULL DEFAULT 0,
  scraped_at       TEXT    NOT NULL DEFAULT '',
  imported_at      TEXT    NOT NULL,
  UNIQUE (library_id, actress, fanha)
);

CREATE INDEX IF NOT EXISTS idx_videos_actress ON videos (library_id, actress);
CREATE INDEX IF NOT EXISTS idx_videos_date    ON videos (library_id, release_date);
CREATE INDEX IF NOT EXISTS idx_videos_title   ON videos (library_id, title);
CREATE INDEX IF NOT EXISTS idx_videos_missing ON videos (library_id, missing);
CREATE INDEX IF NOT EXISTS idx_videos_scraped ON videos (library_id, scraped);

-- 用户数据。主键是业务键而非 videos.id，理由见包注释。
CREATE TABLE IF NOT EXISTS user_data (
  library_id        TEXT    NOT NULL,
  actress           TEXT    NOT NULL,
  fanha             TEXT    NOT NULL,
  favorite          INTEGER NOT NULL DEFAULT 0,
  rating            INTEGER,
  watch_position_ms INTEGER NOT NULL DEFAULT 0,
  play_count        INTEGER NOT NULL DEFAULT 0,
  last_played_at    TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (library_id, actress, fanha)
);

CREATE TABLE IF NOT EXISTS libraries (
  id           TEXT    PRIMARY KEY,
  root         TEXT    NOT NULL,
  last_scan_at TEXT    NOT NULL DEFAULT '',
  video_count  INTEGER NOT NULL DEFAULT 0
);
`

// Store 是索引数据库的句柄。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）索引数据库。
func Open(path string) (*Store, error) {
	// 直接用文件路径而不是 file: URI：Windows 绝对路径在 URI 里的转义规则
	// 很容易出错（C:/... 会被解析成相对路径），而这里并不需要 URI 参数。
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开索引数据库 %s: %w", path, err)
	}

	// 单用户桌面应用，串行访问最省心，也避免了多连接下的 journal 竞争。
	db.SetMaxOpenConns(1)

	// journal_mode 是写进数据库文件的持久设置，开库时设置一次即可。
	//
	// 用 TRUNCATE 而不是 WAL：WAL 会产生 -wal / -shm 附属文件，一旦数据库
	// 因为任何原因落在移动硬盘上，未 checkpoint 就拔盘是公认的损坏来源。
	if _, err := db.Exec("PRAGMA journal_mode=TRUNCATE"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("设置 journal_mode: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close 关闭数据库。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("读取 schema 版本: %w", err)
	}

	switch {
	case current == schemaVersion:
		return nil
	case current > schemaVersion:
		// 数据库来自更新的程序版本。降级运行会把新版本写入的数据弄坏，
		// 所以这里必须拒绝，而不是"尽力而为"。
		return fmt.Errorf(
			"索引数据库版本为 %d，本程序只支持到 %d。请升级程序，或删除该数据库后重新扫描",
			current, schemaVersion,
		)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启迁移事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("建表: %w", err)
	}
	// PRAGMA 不接受占位符参数，只能拼接。schemaVersion 是编译期常量，
	// 不存在注入面。
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("写入 schema 版本: %w", err)
	}
	return tx.Commit()
}

// Reset 清空全部索引数据，保留库文件本身。
//
// 重建索引是产品功能而不是运维手段：用户点了「重建索引」之后，下一次扫描会
// 把一切重新推导出来。user_data 也一并清空 —— 它由用户显式操作触发。
func (s *Store) Reset(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启清空事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range []string{"videos", "user_data"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("清空 %s: %w", table, err)
		}
	}
	return tx.Commit()
}
