package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

var (
	db   *sql.DB
	once sync.Once
)

func Init(dbPath string) error {
	var initErr error
	once.Do(func() {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			initErr = fmt.Errorf("create data dir: %w", err)
			return
		}

		var err error
		db, err = sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
		if err != nil {
			initErr = fmt.Errorf("open database: %w", err)
			return
		}
		db.SetMaxOpenConns(4)  // WAL 模式允许并发读
		db.SetMaxIdleConns(4)
		db.SetConnMaxLifetime(0) // 不超时回收连接

		if err := migrate(); err != nil {
			initErr = fmt.Errorf("migrate: %w", err)
			return
		}
		if err := seedDefaults(); err != nil {
			initErr = fmt.Errorf("seed defaults: %w", err)
		}
	})
	return initErr
}

func DB() *sql.DB {
	return db
}

func migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'user',
			status INTEGER NOT NULL DEFAULT 1,
			qpm INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL DEFAULT 0,
			key TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			status INTEGER NOT NULL DEFAULT 1,
			qpm INTEGER NOT NULL DEFAULT 0,
			channel_ids TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT 'openai',
			base_url TEXT NOT NULL DEFAULT '',
			api_key TEXT NOT NULL DEFAULT '',
			models TEXT NOT NULL DEFAULT '',
			status INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			weight INTEGER NOT NULL DEFAULT 1,
			qpm INTEGER NOT NULL DEFAULT 0,
			timeout INTEGER NOT NULL DEFAULT 300,
			max_retry INTEGER NOT NULL DEFAULT 0,
			custom_instructions TEXT NOT NULL DEFAULT '',
			instructions_position TEXT NOT NULL DEFAULT 'prepend',
			body_modifications TEXT NOT NULL DEFAULT '',
			header_modifications TEXT NOT NULL DEFAULT '',
			used_count INTEGER NOT NULL DEFAULT 0,
			fail_count INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS model_mappings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			route TEXT NOT NULL DEFAULT 'all',
			priority INTEGER NOT NULL DEFAULT 0,
			client_model TEXT NOT NULL DEFAULT '',
			upstream_model TEXT NOT NULL DEFAULT '',
			channel_ids TEXT NOT NULL DEFAULT '',
			status INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL DEFAULT 0,
			api_key_id INTEGER NOT NULL DEFAULT 0,
			channel_id INTEGER NOT NULL DEFAULT 0,
			client_model TEXT NOT NULL DEFAULT '',
			upstream_model TEXT NOT NULL DEFAULT '',
			route TEXT NOT NULL DEFAULT '',
			backend TEXT NOT NULL DEFAULT '',
			stream INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT '',
			error_msg TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_created ON request_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_user ON request_logs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_channel ON request_logs(channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_key ON api_keys(key)`,
		`ALTER TABLE api_keys ADD COLUMN channel_ids TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE api_keys ADD COLUMN remark TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE model_mappings ADD COLUMN name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE model_mappings ADD COLUMN description TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE model_mappings ADD COLUMN route TEXT NOT NULL DEFAULT 'all'`,
		`ALTER TABLE model_mappings ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS idx_model_mappings_lookup ON model_mappings(client_model, route, status, priority DESC)`,
		`ALTER TABLE request_logs ADD COLUMN client_ip TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS request_details (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_log_id INTEGER NOT NULL DEFAULT 0,
			request_headers TEXT NOT NULL DEFAULT '',
			request_body TEXT NOT NULL DEFAULT '',
			prompt TEXT NOT NULL DEFAULT '',
			user_message TEXT NOT NULL DEFAULT '',
			ai_response TEXT NOT NULL DEFAULT '',
			tool_calls TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_details_created ON request_details(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_details_log_id ON request_details(request_log_id)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			if (strings.Contains(stmt, "ALTER TABLE api_keys ADD COLUMN channel_ids") || strings.Contains(stmt, "ALTER TABLE api_keys ADD COLUMN remark") || strings.Contains(stmt, "ALTER TABLE model_mappings ADD COLUMN name") || strings.Contains(stmt, "ALTER TABLE model_mappings ADD COLUMN description") || strings.Contains(stmt, "ALTER TABLE model_mappings ADD COLUMN route") || strings.Contains(stmt, "ALTER TABLE model_mappings ADD COLUMN priority") || strings.Contains(stmt, "ALTER TABLE request_logs ADD COLUMN client_ip")) && strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("exec %q: %w", stmt[:60], err)
		}
	}
	return nil
}

func seedDefaults() error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	log.Println("[DB] 初始化默认管理员用户和 API Key")
	now := time.Now()

	res, err := db.Exec(
		"INSERT INTO users (username, password, role, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"admin", "", "admin", 1, now, now,
	)
	if err != nil {
		return err
	}
	userID, _ := res.LastInsertId()

	_, err = db.Exec(
		"INSERT INTO api_keys (user_id, key, name, remark, status, channel_ids, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		userID, "sk-api2cursor-default", "默认密钥", "系统初始化默认密钥", 1, "", now, now,
	)
	return err
}
