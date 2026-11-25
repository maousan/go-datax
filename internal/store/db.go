package store

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Store 是管理库的入口，持有 GORM 连接。
type Store struct {
	db *gorm.DB
}

// Open 打开（或创建）管理库并执行迁移。
func Open(path string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		TranslateError: true,
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}

	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		return nil, fmt.Errorf("enable WAL on %s: %w", path, err)
	}
	if err := db.AutoMigrate(&DataSource{}, &QueryTemplate{}, &SyncJob{}, &CollectTask{}, &JobRun{}, &PipelineRun{}, &AlertLog{}, &AppSetting{}); err != nil {
		return nil, fmt.Errorf("migrate database %s: %w", path, err)
	}

	return &Store{db: db}, nil
}

// Close 关闭底层连接。
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
