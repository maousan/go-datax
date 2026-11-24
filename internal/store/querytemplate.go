package store

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// 数据集相关错误。
var (
	ErrQueryTemplateNotFound      = errors.New("query template not found")
	ErrDuplicateQueryTemplateName = errors.New("query template name already exists")
)

// QueryTemplate 是可复用的读取端配置。
// SQLText 非空时按 SQL 数据集执行（querySql 模式）；为空时兼容早期的表/列/条件模板。
type QueryTemplate struct {
	ID           int64  `gorm:"primaryKey"`
	Name         string `gorm:"size:100;uniqueIndex;not null"`
	Description  string `gorm:"size:500"`
	DataSourceID int64  `gorm:"index;not null"`
	TableName    string `gorm:"size:200;not null"`
	Columns      string `gorm:"type:text;not null"`
	WhereClause  string `gorm:"type:text"`
	SplitPk      string `gorm:"size:100"`
	SQLText      string `gorm:"type:text"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ColumnList 返回模板的列清单。
func (t QueryTemplate) ColumnList() []string {
	var columns []string
	if err := json.Unmarshal([]byte(t.Columns), &columns); err != nil {
		return nil
	}
	return columns
}

// EncodeQueryColumns 把列清单编码为存储用 JSON。
func EncodeQueryColumns(columns []string) string {
	data, err := json.Marshal(columns)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// ListQueryTemplates 返回数据集列表，query 非空时按名称不区分大小写模糊匹配。
func (s *Store) ListQueryTemplates(query string) ([]QueryTemplate, error) {
	var result []QueryTemplate
	tx := s.db.Order("lower(name) asc")
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		tx = tx.Where("lower(name) LIKE ?", "%"+strings.ToLower(trimmed)+"%")
	}
	if err := tx.Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// GetQueryTemplate 按 id 查询数据集。
func (s *Store) GetQueryTemplate(id int64) (QueryTemplate, error) {
	var template QueryTemplate
	err := s.db.First(&template, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return template, ErrQueryTemplateNotFound
	}
	if err != nil {
		return template, err
	}
	return template, nil
}

// CreateQueryTemplate 创建数据集。
func (s *Store) CreateQueryTemplate(template *QueryTemplate) error {
	exists, err := s.queryTemplateNameExists(template.Name, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateQueryTemplateName
	}
	if err := s.db.Create(template).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateQueryTemplateName
		}
		return err
	}
	return nil
}

// UpdateQueryTemplate 全量保存数据集。
func (s *Store) UpdateQueryTemplate(template *QueryTemplate) error {
	exists, err := s.queryTemplateNameExists(template.Name, template.ID)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateQueryTemplateName
	}
	if err := s.db.Save(template).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateQueryTemplateName
		}
		return err
	}
	return nil
}

// DeleteQueryTemplate 删除数据集。
func (s *Store) DeleteQueryTemplate(id int64) error {
	result := s.db.Delete(&QueryTemplate{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrQueryTemplateNotFound
	}
	return nil
}

// QueryTemplatesUsingDataSource 返回引用指定数据源的数据集。
func (s *Store) QueryTemplatesUsingDataSource(dataSourceID int64) ([]QueryTemplate, error) {
	var result []QueryTemplate
	err := s.db.Where("data_source_id = ?", dataSourceID).Order("lower(name) asc").Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) queryTemplateNameExists(name string, excludeID int64) (bool, error) {
	var count int64
	tx := s.db.Model(&QueryTemplate{}).Where("lower(name) = ?", strings.ToLower(strings.TrimSpace(name)))
	if excludeID != 0 {
		tx = tx.Where("id <> ?", excludeID)
	}
	if err := tx.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
