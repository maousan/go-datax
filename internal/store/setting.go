package store

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// AppSetting 是管理台运行期可修改的配置项。
type AppSetting struct {
	Key       string `gorm:"primaryKey;size:100"`
	Value     string `gorm:"type:text"`
	UpdatedAt time.Time
}

// GetSetting 按 key 读取配置；不存在时 found 为 false。
func (s *Store) GetSetting(key string) (string, bool, error) {
	var setting AppSetting
	err := s.db.Where("key = ?", key).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return setting.Value, true, nil
}

// SaveSetting 写入或覆盖一个配置项。
func (s *Store) SaveSetting(key, value string) error {
	return s.db.Save(&AppSetting{Key: key, Value: value}).Error
}
