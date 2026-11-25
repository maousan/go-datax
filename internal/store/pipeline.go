package store

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrPipelineNotFound        = errors.New("pipeline not found")
	ErrDuplicatePipelineName   = errors.New("pipeline name already exists")
	ErrPipelineVersionNotFound = errors.New("pipeline version not found")
)

// Pipeline is the editable draft of one linear Pipeline.
type Pipeline struct {
	ID                 int64  `gorm:"primaryKey"`
	Name               string `gorm:"size:100;uniqueIndex;not null"`
	Description        string `gorm:"size:500"`
	SourceID           int64  `gorm:"index;not null"`
	SinkID             int64  `gorm:"index;not null"`
	TransformsJSON     string `gorm:"column:transforms_json;type:text"`
	DraftSourceJSON    string `gorm:"column:draft_source_json;type:text"`
	DraftSinkJSON      string `gorm:"column:draft_sink_json;type:text"`
	DraftVersionID     *int64 `gorm:"column:draft_version_id"`
	DraftVersionNumber int    `gorm:"column:draft_version_number;not null;default:0"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// PipelineVersion is an immutable published snapshot.
type PipelineVersion struct {
	ID                      int64  `gorm:"primaryKey"`
	PipelineID              int64  `gorm:"not null;uniqueIndex:idx_pipeline_version"`
	VersionNumber           int    `gorm:"not null;uniqueIndex:idx_pipeline_version"`
	SourceEndpointID        int64  `gorm:"index"`
	SinkEndpointID          int64  `gorm:"index"`
	SourceDataSourceID      int64  `gorm:"index"`
	SinkDataSourceID        int64  `gorm:"index"`
	SourceName              string `gorm:"size:100"`
	SinkName                string `gorm:"size:100"`
	Summary                 string `gorm:"size:300"`
	SourceJSON              string `gorm:"type:text;not null"`
	SinkJSON                string `gorm:"type:text;not null"`
	TransformsJSON          string `gorm:"column:transforms_json;type:text"`
	TransformLibraryVersion int    `gorm:"not null;default:0"`
	CreatedAt               time.Time
}

// ListPipelines returns pipelines ordered by name.
func (s *Store) ListPipelines(query string) ([]Pipeline, error) {
	var result []Pipeline
	tx := s.db.Order("lower(name) asc")
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		tx = tx.Where("lower(name) LIKE ?", "%"+strings.ToLower(trimmed)+"%")
	}
	if err := tx.Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// GetPipeline returns one pipeline.
func (s *Store) GetPipeline(id int64) (Pipeline, error) {
	var pipeline Pipeline
	err := s.db.First(&pipeline, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return pipeline, ErrPipelineNotFound
	}
	if err != nil {
		return pipeline, err
	}
	return pipeline, nil
}

// CreatePipeline inserts a draft.
func (s *Store) CreatePipeline(pipeline *Pipeline) error {
	exists, err := s.pipelineNameExists(pipeline.Name, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicatePipelineName
	}
	if err := s.db.Create(pipeline).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicatePipelineName
		}
		return err
	}
	return nil
}

// UpdatePipeline saves the editable fields of a draft.
func (s *Store) UpdatePipeline(pipeline *Pipeline) error {
	exists, err := s.pipelineNameExists(pipeline.Name, pipeline.ID)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicatePipelineName
	}
	if err := s.db.Save(pipeline).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicatePipelineName
		}
		return err
	}
	return nil
}

// CreatePipelineVersion assigns the next version number and inserts the snapshot.
func (s *Store) CreatePipelineVersion(version *PipelineVersion) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var latest int
		if err := tx.Model(&PipelineVersion{}).
			Where("pipeline_id = ?", version.PipelineID).
			Select("COALESCE(MAX(version_number), 0)").
			Scan(&latest).Error; err != nil {
			return err
		}
		version.VersionNumber = latest + 1
		return tx.Create(version).Error
	})
}

// ListPipelineVersions returns versions newest first.
func (s *Store) ListPipelineVersions(pipelineID int64) ([]PipelineVersion, error) {
	var result []PipelineVersion
	err := s.db.Where("pipeline_id = ?", pipelineID).
		Order("version_number desc").
		Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetPipelineVersion returns one version by id.
func (s *Store) GetPipelineVersion(id int64) (PipelineVersion, error) {
	var version PipelineVersion
	err := s.db.First(&version, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return version, ErrPipelineVersionNotFound
	}
	if err != nil {
		return version, err
	}
	return version, nil
}

// LatestPipelineVersion returns the highest published version, if any.
func (s *Store) LatestPipelineVersion(pipelineID int64) (PipelineVersion, error) {
	var version PipelineVersion
	err := s.db.Where("pipeline_id = ?", pipelineID).
		Order("version_number desc").
		First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return version, ErrPipelineVersionNotFound
	}
	if err != nil {
		return version, err
	}
	return version, nil
}

// PipelinesUsingEndpoint returns drafts that reference a Source or Sink.
func (s *Store) PipelinesUsingEndpoint(endpointID int64) ([]Pipeline, error) {
	var result []Pipeline
	err := s.db.Where("source_id = ? OR sink_id = ?", endpointID, endpointID).
		Order("lower(name) asc").
		Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PipelineVersionsUsingEndpoint returns published versions that reference an endpoint.
func (s *Store) PipelineVersionsUsingEndpoint(endpointID int64) ([]PipelineVersion, error) {
	var result []PipelineVersion
	err := s.db.Where("source_endpoint_id = ? OR sink_endpoint_id = ?", endpointID, endpointID).
		Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PipelineVersionsUsingDataSource returns published versions that reference a data source.
func (s *Store) PipelineVersionsUsingDataSource(dataSourceID int64) ([]PipelineVersion, error) {
	var result []PipelineVersion
	err := s.db.Where("source_data_source_id = ? OR sink_data_source_id = ?", dataSourceID, dataSourceID).
		Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetEndpointsByIDs returns endpoints in one query.
func (s *Store) GetEndpointsByIDs(ids []int64) ([]Endpoint, error) {
	if len(ids) == 0 {
		return []Endpoint{}, nil
	}
	var result []Endpoint
	err := s.db.Where("id IN ?", ids).Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetDataSourcesByIDs returns data sources in one query.
func (s *Store) GetDataSourcesByIDs(ids []int64) ([]DataSource, error) {
	if len(ids) == 0 {
		return []DataSource{}, nil
	}
	var result []DataSource
	err := s.db.Where("id IN ?", ids).Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

// LatestPipelineVersionsByPipelineIDs returns one latest version per pipeline.
func (s *Store) LatestPipelineVersionsByPipelineIDs(ids []int64) ([]PipelineVersion, error) {
	if len(ids) == 0 {
		return []PipelineVersion{}, nil
	}
	var result []PipelineVersion
	subquery := s.db.Model(&PipelineVersion{}).
		Select("MAX(id)").
		Where("pipeline_id IN ?", ids).
		Group("pipeline_id")
	err := s.db.Where("id IN (?)", subquery).Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

// LatestPipelineRunsByPipelineIDs returns one latest run per pipeline.
func (s *Store) LatestPipelineRunsByPipelineIDs(ids []int64) ([]PipelineRun, error) {
	if len(ids) == 0 {
		return []PipelineRun{}, nil
	}
	var result []PipelineRun
	subquery := s.db.Model(&PipelineRun{}).
		Select("MAX(id)").
		Where("pipeline_id IN ?", ids).
		Group("pipeline_id")
	err := s.db.Where("id IN (?)", subquery).Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) pipelineNameExists(name string, excludeID int64) (bool, error) {
	var count int64
	tx := s.db.Model(&Pipeline{}).Where("lower(name) = ?", strings.ToLower(strings.TrimSpace(name)))
	if excludeID != 0 {
		tx = tx.Where("id <> ?", excludeID)
	}
	if err := tx.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
