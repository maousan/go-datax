package store

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ErrDataSourceNotFound 表示数据源不存在。
var ErrDataSourceNotFound = errors.New("data source not found")

// ErrDuplicateDataSourceName 表示数据源名称重复。
var ErrDuplicateDataSourceName = errors.New("data source name already exists")

// StringMap 以 JSON 文本形式存储在数据库中。
type StringMap map[string]string

// Value 实现 driver.Valuer。
func (m StringMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

// Scan 实现 sql.Scanner。
func (m *StringMap) Scan(value any) error {
	if value == nil {
		*m = StringMap{}
		return nil
	}

	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("unsupported properties type %T", value)
	}

	result := StringMap{}
	if len(raw) == 0 {
		*m = result
		return nil
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	*m = result
	return nil
}

// DataSource 是一个可复用的数据源连接定义。
type DataSource struct {
	ID         int64     `gorm:"primaryKey"`
	Name       string    `gorm:"size:100;uniqueIndex;not null"`
	Type       string    `gorm:"size:20;not null"`
	JDBCUrl    string    `gorm:"column:jdbc_url;size:500;not null"`
	Username   string    `gorm:"size:200"`
	Password   string    `gorm:"size:500"`
	Version    string    `gorm:"size:500"`
	Properties StringMap `gorm:"type:text"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ListDataSources 返回数据源列表，query 非空时按名称不区分大小写模糊匹配。
func (s *Store) ListDataSources(query string) ([]DataSource, error) {
	var result []DataSource
	tx := s.db.Order("lower(name) asc")

	if trimmed := strings.TrimSpace(query); trimmed != "" {
		tx = tx.Where("lower(name) LIKE ?", "%"+strings.ToLower(trimmed)+"%")
	}

	if err := tx.Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// GetDataSource 按 id 查询数据源。
func (s *Store) GetDataSource(id int64) (DataSource, error) {
	var ds DataSource
	err := s.db.First(&ds, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ds, ErrDataSourceNotFound
	}
	if err != nil {
		return ds, err
	}
	return ds, nil
}

// CountDataSources 返回数据源总数。
func (s *Store) CountDataSources() (int64, error) {
	var count int64
	if err := s.db.Model(&DataSource{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// UpdateDataSourceVersion 只更新版本号，不改变 updated_at。
func (s *Store) UpdateDataSourceVersion(id int64, version string) error {
	return s.db.Model(&DataSource{}).
		Where("id = ?", id).
		UpdateColumn("version", version).Error
}

// CreateDataSource 创建数据源，名称冲突时返回 ErrDuplicateDataSourceName。
func (s *Store) CreateDataSource(ds *DataSource) error {
	exists, err := s.dataSourceNameExists(ds.Name, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateDataSourceName
	}

	if err := s.db.Create(ds).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateDataSourceName
		}
		return err
	}
	return nil
}

// UpdateDataSource 全量保存数据源。
func (s *Store) UpdateDataSource(ds *DataSource) error {
	exists, err := s.dataSourceNameExists(ds.Name, ds.ID)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateDataSourceName
	}

	if err := s.db.Save(ds).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateDataSourceName
		}
		return err
	}
	return nil
}

// dataSourceNameExists 检查名称是否已被占用（不区分大小写）。
func (s *Store) dataSourceNameExists(name string, excludeID int64) (bool, error) {
	var count int64
	tx := s.db.Model(&DataSource{}).
		Where("lower(name) = ?", strings.ToLower(strings.TrimSpace(name)))
	if excludeID != 0 {
		tx = tx.Where("id <> ?", excludeID)
	}
	if err := tx.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteDataSource 删除数据源，不存在时返回 ErrDataSourceNotFound。
func (s *Store) DeleteDataSource(id int64) error {
	result := s.db.Delete(&DataSource{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrDataSourceNotFound
	}
	return nil
}
