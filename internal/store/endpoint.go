package store

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	EndpointKindSource = "source"
	EndpointKindSink   = "sink"
)

var (
	ErrEndpointNotFound      = errors.New("endpoint not found")
	ErrDuplicateEndpointName = errors.New("endpoint name already exists")
)

// Endpoint is a reusable Source or Sink definition. Sources and sinks share a
// table so pipeline references use one ID space; Kind keeps them apart.
type Endpoint struct {
	ID           int64     `gorm:"primaryKey"`
	Kind         string    `gorm:"size:10;not null;uniqueIndex:idx_endpoint_kind_name"`
	Name         string    `gorm:"size:100;not null;uniqueIndex:idx_endpoint_kind_name"`
	Description  string    `gorm:"size:500"`
	Connector    string    `gorm:"size:20;not null"`
	DataSourceID int64     `gorm:"index;not null"`
	TableName    string    `gorm:"size:200"`
	QueryText    string    `gorm:"column:query_text;type:text"`
	Path         string    `gorm:"size:1000"`
	SchemaJSON   string    `gorm:"column:schema_json;type:text;not null"`
	Options      StringMap `gorm:"type:text"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func ValidEndpointKind(kind string) bool {
	return kind == EndpointKindSource || kind == EndpointKindSink
}

// ListEndpoints returns one kind, ordered by name, optionally filtered.
func (s *Store) ListEndpoints(kind, query string) ([]Endpoint, error) {
	var result []Endpoint
	tx := s.db.Where("kind = ?", kind).Order("lower(name) asc")
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		tx = tx.Where("lower(name) LIKE ?", "%"+strings.ToLower(trimmed)+"%")
	}
	if err := tx.Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// GetEndpoint returns one endpoint of the requested kind.
func (s *Store) GetEndpoint(kind string, id int64) (Endpoint, error) {
	var endpoint Endpoint
	err := s.db.Where("kind = ? AND id = ?", kind, id).First(&endpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return endpoint, ErrEndpointNotFound
	}
	if err != nil {
		return endpoint, err
	}
	return endpoint, nil
}

// CreateEndpoint inserts a source or sink definition.
func (s *Store) CreateEndpoint(endpoint *Endpoint) error {
	exists, err := s.endpointNameExists(endpoint.Kind, endpoint.Name, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateEndpointName
	}
	if err := s.db.Create(endpoint).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateEndpointName
		}
		return err
	}
	return nil
}

// UpdateEndpoint saves an existing source or sink definition.
func (s *Store) UpdateEndpoint(endpoint *Endpoint) error {
	exists, err := s.endpointNameExists(endpoint.Kind, endpoint.Name, endpoint.ID)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateEndpointName
	}
	if err := s.db.Save(endpoint).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateEndpointName
		}
		return err
	}
	return nil
}

// DeleteEndpoint deletes one endpoint of the requested kind.
func (s *Store) DeleteEndpoint(kind string, id int64) error {
	result := s.db.Where("kind = ?", kind).Delete(&Endpoint{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrEndpointNotFound
	}
	return nil
}

// EndpointsUsingDataSource returns all sources and sinks that reference a data source.
func (s *Store) EndpointsUsingDataSource(dataSourceID int64) ([]Endpoint, error) {
	var result []Endpoint
	err := s.db.Where("data_source_id = ?", dataSourceID).
		Order("kind asc, lower(name) asc").
		Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) endpointNameExists(kind, name string, excludeID int64) (bool, error) {
	var count int64
	tx := s.db.Model(&Endpoint{}).
		Where("kind = ? AND lower(name) = ?", kind, strings.ToLower(strings.TrimSpace(name)))
	if excludeID != 0 {
		tx = tx.Where("id <> ?", excludeID)
	}
	if err := tx.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
