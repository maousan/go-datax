package store

import (
	"database/sql"
	"fmt"

	_ "github.com/glebarez/go-sqlite"
)

// EnsureDatabase 创建（或打开）管理库并初始化基础设置。
// 表结构由后续票据的迁移负责，这里只保证文件可用且启用 WAL。
func EnsureDatabase(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open database %s: %w", path, err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("initialize database %s: %w", path, err)
	}
	return nil
}
