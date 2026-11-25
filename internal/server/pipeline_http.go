package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/connector"
	"github.com/longkeyy/go-datax/internal/store"
	"github.com/longkeyy/go-datax/internal/transform"
	"github.com/longkeyy/go-datax/internal/worker"
)

type pipelineRequest struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	SourceID    int64            `json:"sourceId"`
	SinkID      int64            `json:"sinkId"`
	Transforms  []transform.Step `json:"transforms"`
}

type transformPreviewRequest struct {
	SourceID     int64            `json:"sourceId"`
	SourceSchema connector.Schema `json:"sourceSchema"`
	Transforms   []transform.Step `json:"transforms"`
}

type pipelineEndpointSummary struct {
	ID             int64             `json:"id"`
	Name           string            `json:"name"`
	Connector      string            `json:"connector"`
	DataSourceName string            `json:"dataSourceName"`
	Table          string            `json:"table"`
	Query          string            `json:"query"`
	Path           string            `json:"path"`
	Schema         connector.Schema  `json:"schema"`
	Options        map[string]string `json:"options"`
}

type pipelineVersionSummary struct {
	ID            int64  `json:"id"`
	VersionNumber int    `json:"versionNumber"`
	SourceName    string `json:"sourceName"`
	SinkName      string `json:"sinkName"`
	Summary       string `json:"summary"`
	CreatedAt     string `json:"createdAt"`
}

type pipelineResponse struct {
	ID                 int64                    `json:"id"`
	Name               string                   `json:"name"`
	Description        string                   `json:"description"`
	SourceID           int64                    `json:"sourceId"`
	SinkID             int64                    `json:"sinkId"`
	Source             *pipelineEndpointSummary `json:"source"`
	Sink               *pipelineEndpointSummary `json:"sink"`
	Transforms         []transform.Step         `json:"transforms"`
	TransformPreview   *transform.Preview       `json:"transformPreview"`
	DraftVersionNumber int                      `json:"draftVersionNumber"`
	LatestVersion      *pipelineVersionSummary  `json:"latestVersion"`
	LastRun            *pipelineRunResponse     `json:"lastRun"`
	CreatedAt          string                   `json:"createdAt"`
	UpdatedAt          string                   `json:"updatedAt"`
}

type pipelineEndpointSnapshot struct {
	EndpointID     int64             `json:"endpointId"`
	Name           string            `json:"name"`
	Connector      string            `json:"connector"`
	DataSourceID   int64             `json:"dataSourceId"`
	DataSourceName string            `json:"dataSourceName"`
	Table          string            `json:"table"`
	Query          string            `json:"query"`
	Path           string            `json:"path"`
	Schema         connector.Schema  `json:"schema"`
	Options        map[string]string `json:"options"`
}

type pipelineVersionResponse struct {
	ID                      int64                    `json:"id"`
	PipelineID              int64                    `json:"pipelineId"`
	VersionNumber           int                      `json:"versionNumber"`
	SourceName              string                   `json:"sourceName"`
	SinkName                string                   `json:"sinkName"`
	Summary                 string                   `json:"summary"`
	Source                  pipelineEndpointSnapshot `json:"source"`
	Sink                    pipelineEndpointSnapshot `json:"sink"`
	Transforms              []transform.Step         `json:"transforms"`
	TransformLibraryVersion int                      `json:"transformLibraryVersion"`
	CreatedAt               string                   `json:"createdAt"`
}

func (s pipelineEndpointSnapshot) connectorConfig(dataSource store.DataSource) connector.Config {
	return buildConnectorConfig(dataSource, s.Connector, s.Schema, s.Options, s.Table, s.Query, s.Path)
}

func (s pipelineEndpointSnapshot) workerEndpoint(dataSource store.DataSource) worker.EndpointSpec {
	config := s.connectorConfig(dataSource)
	return worker.EndpointSpec{
		Name:                 s.Name,
		Connector:            config.Connector,
		DataSourceType:       config.DataSourceType,
		JDBCUrl:              config.JDBCUrl,
		DataSourceProperties: config.DataSourceProperties,
		Schema:               config.Schema,
		Options:              config.Options,
	}
}

func normalizePipelineRequest(req pipelineRequest) (pipelineRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.Name == "" {
		return req, fmt.Errorf("名称不能为空")
	}
	if len([]rune(req.Name)) > 100 {
		return req, fmt.Errorf("名称不能超过 100 个字符")
	}
	if len([]rune(req.Description)) > 500 {
		return req, fmt.Errorf("说明不能超过 500 个字符")
	}
	if req.SourceID <= 0 {
		return req, fmt.Errorf("必须选择源")
	}
	if req.SinkID <= 0 {
		return req, fmt.Errorf("必须选择目标")
	}
	return req, nil
}

func decodeEndpointSchema(endpoint store.Endpoint) (connector.Schema, error) {
	var schema connector.Schema
	if err := json.Unmarshal([]byte(endpoint.SchemaJSON), &schema); err != nil {
		return nil, fmt.Errorf("解析字段失败: %w", err)
	}
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("字段定义不正确: %w", err)
	}
	return schema, nil
}

func decodeTransforms(raw string) ([]transform.Step, error) {
	if strings.TrimSpace(raw) == "" {
		return []transform.Step{}, nil
	}
	var steps []transform.Step
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return nil, err
	}
	if steps == nil {
		steps = []transform.Step{}
	}
	return steps, nil
}

func encodeTransforms(steps []transform.Step) (string, error) {
	if steps == nil {
		steps = []transform.Step{}
	}
	data, err := json.Marshal(steps)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func validatePipelineTransforms(source store.Endpoint, steps []transform.Step) (transform.Preview, error) {
	sourceSchema, err := decodeEndpointSchema(source)
	if err != nil {
		return transform.Preview{}, fmt.Errorf("源字段不正确: %w", err)
	}
	return transform.Validate(sourceSchema, steps)
}

func buildPipelineSnapshot(endpoint store.Endpoint, dataSource store.DataSource) (pipelineEndpointSnapshot, error) {
	schema, err := decodeEndpointSchema(endpoint)
	if err != nil {
		return pipelineEndpointSnapshot{}, err
	}
	options := map[string]string{}
	for key, value := range endpoint.Options {
		options[key] = value
	}
	return pipelineEndpointSnapshot{
		EndpointID:     endpoint.ID,
		Name:           endpoint.Name,
		Connector:      endpoint.Connector,
		DataSourceID:   endpoint.DataSourceID,
		DataSourceName: dataSource.Name,
		Table:          endpoint.TableName,
		Query:          endpoint.QueryText,
		Path:           endpoint.Path,
		Schema:         schema,
		Options:        options,
	}, nil
}

func toPipelineEndpointSummary(endpoint store.Endpoint, dataSourceName string) (pipelineEndpointSummary, error) {
	var schema connector.Schema
	if err := json.Unmarshal([]byte(endpoint.SchemaJSON), &schema); err != nil {
		return pipelineEndpointSummary{}, err
	}
	options := map[string]string{}
	for key, value := range endpoint.Options {
		options[key] = value
	}
	return pipelineEndpointSummary{
		ID:             endpoint.ID,
		Name:           endpoint.Name,
		Connector:      endpoint.Connector,
		DataSourceName: dataSourceName,
		Table:          endpoint.TableName,
		Query:          endpoint.QueryText,
		Path:           endpoint.Path,
		Schema:         schema,
		Options:        options,
	}, nil
}

func toPipelineEndpointSummaryFromSnapshot(snapshot pipelineEndpointSnapshot, dataSourceName string) pipelineEndpointSummary {
	options := map[string]string{}
	for key, value := range snapshot.Options {
		options[key] = value
	}
	return pipelineEndpointSummary{
		ID:             snapshot.EndpointID,
		Name:           snapshot.Name,
		Connector:      snapshot.Connector,
		DataSourceName: dataSourceName,
		Table:          snapshot.Table,
		Query:          snapshot.Query,
		Path:           snapshot.Path,
		Schema:         snapshot.Schema,
		Options:        options,
	}
}

func toPipelineVersionSummary(version store.PipelineVersion) pipelineVersionSummary {
	return pipelineVersionSummary{
		ID:            version.ID,
		VersionNumber: version.VersionNumber,
		SourceName:    version.SourceName,
		SinkName:      version.SinkName,
		Summary:       version.Summary,
		CreatedAt:     version.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func toPipelineVersionResponse(version store.PipelineVersion) (pipelineVersionResponse, error) {
	var source, sink pipelineEndpointSnapshot
	if err := json.Unmarshal([]byte(version.SourceJSON), &source); err != nil {
		return pipelineVersionResponse{}, err
	}
	if err := json.Unmarshal([]byte(version.SinkJSON), &sink); err != nil {
		return pipelineVersionResponse{}, err
	}
	transforms, err := decodeTransforms(version.TransformsJSON)
	if err != nil {
		return pipelineVersionResponse{}, err
	}
	return pipelineVersionResponse{
		ID:                      version.ID,
		PipelineID:              version.PipelineID,
		VersionNumber:           version.VersionNumber,
		SourceName:              version.SourceName,
		SinkName:                version.SinkName,
		Summary:                 version.Summary,
		Source:                  source,
		Sink:                    sink,
		Transforms:              transforms,
		TransformLibraryVersion: version.TransformLibraryVersion,
		CreatedAt:               version.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func pipelineResponseFrom(
	pipeline store.Pipeline,
	source, sink store.Endpoint,
	sourceDSName, sinkDSName string,
	sourceSnapshot, sinkSnapshot *pipelineEndpointSnapshot,
	latest *store.PipelineVersion,
	last *store.PipelineRun,
) (pipelineResponse, error) {
	response := pipelineResponse{
		ID:                 pipeline.ID,
		Name:               pipeline.Name,
		Description:        pipeline.Description,
		SourceID:           pipeline.SourceID,
		SinkID:             pipeline.SinkID,
		DraftVersionNumber: pipeline.DraftVersionNumber,
		CreatedAt:          pipeline.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          pipeline.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if sourceSnapshot != nil {
		summary := toPipelineEndpointSummaryFromSnapshot(*sourceSnapshot, sourceDSName)
		response.Source = &summary
	} else {
		sourceSummary, err := toPipelineEndpointSummary(source, sourceDSName)
		if err != nil {
			return response, err
		}
		response.Source = &sourceSummary
	}
	if sinkSnapshot != nil {
		summary := toPipelineEndpointSummaryFromSnapshot(*sinkSnapshot, sinkDSName)
		response.Sink = &summary
	} else {
		sinkSummary, err := toPipelineEndpointSummary(sink, sinkDSName)
		if err != nil {
			return response, err
		}
		response.Sink = &sinkSummary
	}
	transforms, err := decodeTransforms(pipeline.TransformsJSON)
	if err != nil {
		return response, err
	}
	response.Transforms = transforms
	var input connector.Schema
	if sourceSnapshot != nil {
		input = sourceSnapshot.Schema
	} else {
		input, err = decodeEndpointSchema(source)
		if err != nil {
			return response, err
		}
	}
	preview, err := transform.Validate(input, transforms)
	if err == nil {
		response.TransformPreview = &preview
	}
	// A draft can go stale when its source schema changes; keep the pipeline
	// readable and let the transform preview surface the validation error.
	if latest != nil {
		summary := toPipelineVersionSummary(*latest)
		response.LatestVersion = &summary
	}
	if last != nil {
		run := toPipelineRunResponse(*last)
		response.LastRun = &run
	}
	return response, nil
}

func pipelineDraftSnapshots(pipeline store.Pipeline) (*pipelineEndpointSnapshot, *pipelineEndpointSnapshot, error) {
	if strings.TrimSpace(pipeline.DraftSourceJSON) == "" || strings.TrimSpace(pipeline.DraftSinkJSON) == "" {
		return nil, nil, nil
	}
	var source, sink pipelineEndpointSnapshot
	if err := json.Unmarshal([]byte(pipeline.DraftSourceJSON), &source); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal([]byte(pipeline.DraftSinkJSON), &sink); err != nil {
		return nil, nil, err
	}
	return &source, &sink, nil
}

func (s *Server) loadPipelineResponse(pipeline store.Pipeline) (pipelineResponse, error) {
	source, err := s.dataStore.GetEndpoint(store.EndpointKindSource, pipeline.SourceID)
	if err != nil {
		return pipelineResponse{}, err
	}
	sink, err := s.dataStore.GetEndpoint(store.EndpointKindSink, pipeline.SinkID)
	if err != nil {
		return pipelineResponse{}, err
	}
	sourceDS, err := s.dataStore.GetDataSource(source.DataSourceID)
	if err != nil {
		return pipelineResponse{}, err
	}
	sinkDS, err := s.dataStore.GetDataSource(sink.DataSourceID)
	if err != nil {
		return pipelineResponse{}, err
	}
	sourceSnapshot, sinkSnapshot, err := pipelineDraftSnapshots(pipeline)
	if err != nil {
		return pipelineResponse{}, err
	}
	sourceDSName := sourceDS.Name
	if sourceSnapshot != nil {
		sourceDSName = sourceSnapshot.DataSourceName
	}
	sinkDSName := sinkDS.Name
	if sinkSnapshot != nil {
		sinkDSName = sinkSnapshot.DataSourceName
	}
	var latest *store.PipelineVersion
	version, err := s.dataStore.LatestPipelineVersion(pipeline.ID)
	if err == nil {
		latest = &version
	} else if !errors.Is(err, store.ErrPipelineVersionNotFound) {
		return pipelineResponse{}, err
	}
	var last *store.PipelineRun
	runs, _, err := s.dataStore.ListPipelineRuns(store.PipelineRunFilter{
		PipelineID: pipeline.ID,
		Page:       1,
		PageSize:   1,
	})
	if err != nil {
		return pipelineResponse{}, err
	}
	if len(runs) > 0 {
		last = &runs[0]
	}
	return pipelineResponseFrom(pipeline, source, sink, sourceDSName, sinkDSName, sourceSnapshot, sinkSnapshot, latest, last)
}

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	s.prunePipelineRuns()
	pipelines, err := s.dataStore.ListPipelines(r.URL.Query().Get("query"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	response := make([]pipelineResponse, 0, len(pipelines))
	if len(pipelines) == 0 {
		writeJSON(w, http.StatusOK, response)
		return
	}

	pipelineIDs := make([]int64, 0, len(pipelines))
	endpointIDs := make([]int64, 0, len(pipelines)*2)
	for _, pipeline := range pipelines {
		pipelineIDs = append(pipelineIDs, pipeline.ID)
		endpointIDs = append(endpointIDs, pipeline.SourceID, pipeline.SinkID)
	}
	endpoints, err := s.dataStore.GetEndpointsByIDs(endpointIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	endpointByID := make(map[int64]store.Endpoint, len(endpoints))
	dataSourceIDs := make([]int64, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpointByID[endpoint.ID] = endpoint
		dataSourceIDs = append(dataSourceIDs, endpoint.DataSourceID)
	}
	dataSources, err := s.dataStore.GetDataSourcesByIDs(dataSourceIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	dataSourceByID := make(map[int64]store.DataSource, len(dataSources))
	for _, dataSource := range dataSources {
		dataSourceByID[dataSource.ID] = dataSource
	}
	versions, err := s.dataStore.LatestPipelineVersionsByPipelineIDs(pipelineIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	versionByPipeline := make(map[int64]store.PipelineVersion, len(versions))
	for _, version := range versions {
		versionByPipeline[version.PipelineID] = version
	}
	runs, err := s.dataStore.LatestPipelineRunsByPipelineIDs(pipelineIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	runByPipeline := make(map[int64]store.PipelineRun, len(runs))
	for _, run := range runs {
		if run.PipelineID != nil {
			runByPipeline[*run.PipelineID] = run
		}
	}

	for _, pipeline := range pipelines {
		source, sourceOK := endpointByID[pipeline.SourceID]
		sink, sinkOK := endpointByID[pipeline.SinkID]
		if !sourceOK || !sinkOK {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
			return
		}
		sourceDS, sourceDSOK := dataSourceByID[source.DataSourceID]
		sinkDS, sinkDSOK := dataSourceByID[sink.DataSourceID]
		if !sourceDSOK || !sinkDSOK {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
			return
		}
		var latest *store.PipelineVersion
		if version, ok := versionByPipeline[pipeline.ID]; ok {
			latest = &version
		}
		var last *store.PipelineRun
		if run, ok := runByPipeline[pipeline.ID]; ok {
			last = &run
		}
		var sourceSnapshot, sinkSnapshot *pipelineEndpointSnapshot
		sourceSnapshot, sinkSnapshot, err = pipelineDraftSnapshots(pipeline)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
			return
		}
		sourceDSName := sourceDS.Name
		if sourceSnapshot != nil {
			sourceDSName = sourceSnapshot.DataSourceName
		}
		sinkDSName := sinkDS.Name
		if sinkSnapshot != nil {
			sinkDSName = sinkSnapshot.DataSourceName
		}
		item, err := pipelineResponseFrom(pipeline, source, sink, sourceDSName, sinkDSName, sourceSnapshot, sinkSnapshot, latest, last)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
			return
		}
		response = append(response, item)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	s.prunePipelineRuns()
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	pipeline, err := s.dataStore.GetPipeline(id)
	if errors.Is(err, store.ErrPipelineNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Pipeline 不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	response, err := s.loadPipelineResponse(pipeline)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var req pipelineRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := normalizePipelineRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	source, _, ok := s.loadPipelineEndpoints(w, normalized.SourceID, normalized.SinkID)
	if !ok {
		return
	}
	if _, err := validatePipelineTransforms(source, normalized.Transforms); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_transform", err.Error())
		return
	}
	transformsJSON, err := encodeTransforms(normalized.Transforms)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "保存 Transform 失败")
		return
	}
	pipeline := &store.Pipeline{
		Name:           normalized.Name,
		Description:    normalized.Description,
		SourceID:       normalized.SourceID,
		SinkID:         normalized.SinkID,
		TransformsJSON: transformsJSON,
	}
	if err := s.dataStore.CreatePipeline(pipeline); err != nil {
		if errors.Is(err, store.ErrDuplicatePipelineName) {
			writeError(w, http.StatusConflict, "duplicate_name", "Pipeline 名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "创建 Pipeline 失败")
		return
	}
	response, err := s.loadPipelineResponse(*pipeline)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	pipeline, err := s.dataStore.GetPipeline(id)
	if errors.Is(err, store.ErrPipelineNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Pipeline 不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	var req pipelineRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := normalizePipelineRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	source, _, ok := s.loadPipelineEndpoints(w, normalized.SourceID, normalized.SinkID)
	if !ok {
		return
	}
	if pipeline.SourceID == normalized.SourceID && strings.TrimSpace(pipeline.DraftSourceJSON) != "" {
		var snapshot pipelineEndpointSnapshot
		if err := json.Unmarshal([]byte(pipeline.DraftSourceJSON), &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取草稿快照失败")
			return
		}
		if _, err := transform.Validate(snapshot.Schema, normalized.Transforms); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_transform", err.Error())
			return
		}
	} else if _, err := validatePipelineTransforms(source, normalized.Transforms); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_transform", err.Error())
		return
	}
	transformsJSON, err := encodeTransforms(normalized.Transforms)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "保存 Transform 失败")
		return
	}
	pipeline.Name = normalized.Name
	pipeline.Description = normalized.Description
	if pipeline.SourceID != normalized.SourceID || pipeline.SinkID != normalized.SinkID {
		pipeline.DraftSourceJSON = ""
		pipeline.DraftSinkJSON = ""
		pipeline.DraftVersionID = nil
		pipeline.DraftVersionNumber = 0
	}
	pipeline.SourceID = normalized.SourceID
	pipeline.SinkID = normalized.SinkID
	pipeline.TransformsJSON = transformsJSON
	if err := s.dataStore.UpdatePipeline(&pipeline); err != nil {
		if errors.Is(err, store.ErrDuplicatePipelineName) {
			writeError(w, http.StatusConflict, "duplicate_name", "Pipeline 名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "更新 Pipeline 失败")
		return
	}
	response, err := s.loadPipelineResponse(pipeline)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) loadPipelineEndpoints(w http.ResponseWriter, sourceID, sinkID int64) (store.Endpoint, store.Endpoint, bool) {
	source, err := s.dataStore.GetEndpoint(store.EndpointKindSource, sourceID)
	if err != nil {
		if errors.Is(err, store.ErrEndpointNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_request", "源不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取源失败")
		}
		return store.Endpoint{}, store.Endpoint{}, false
	}
	sink, err := s.dataStore.GetEndpoint(store.EndpointKindSink, sinkID)
	if err != nil {
		if errors.Is(err, store.ErrEndpointNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_request", "目标不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取目标失败")
		}
		return store.Endpoint{}, store.Endpoint{}, false
	}
	return source, sink, true
}

func (s *Server) handleTransformPreview(w http.ResponseWriter, r *http.Request) {
	var req transformPreviewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	if req.SourceID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "必须选择源")
		return
	}
	var schema connector.Schema
	if len(req.SourceSchema) > 0 {
		schema = req.SourceSchema
	} else {
		source, err := s.dataStore.GetEndpoint(store.EndpointKindSource, req.SourceID)
		if errors.Is(err, store.ErrEndpointNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_request", "源不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取源失败")
			return
		}
		schema, err = decodeEndpointSchema(source)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	preview, err := transform.Validate(schema, req.Transforms)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_transform", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handlePublishPipeline(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	pipeline, err := s.dataStore.GetPipeline(id)
	if errors.Is(err, store.ErrPipelineNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Pipeline 不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	source, err := s.dataStore.GetEndpoint(store.EndpointKindSource, pipeline.SourceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "源不存在")
		return
	}
	sink, err := s.dataStore.GetEndpoint(store.EndpointKindSink, pipeline.SinkID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "目标不存在")
		return
	}
	sourceSnapshot, sinkSnapshot, err := pipelineDraftSnapshots(pipeline)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取草稿快照失败")
		return
	}
	sourceDataSourceID := source.DataSourceID
	if sourceSnapshot != nil {
		sourceDataSourceID = sourceSnapshot.DataSourceID
	}
	sinkDataSourceID := sink.DataSourceID
	if sinkSnapshot != nil {
		sinkDataSourceID = sinkSnapshot.DataSourceID
	}
	sourceDS, err := s.dataStore.GetDataSource(sourceDataSourceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "源的数据源不存在")
		return
	}
	sinkDS, err := s.dataStore.GetDataSource(sinkDataSourceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "目标的数据源不存在")
		return
	}
	if sourceSnapshot == nil {
		snapshot, err := buildPipelineSnapshot(source, sourceDS)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_pipeline", err.Error())
			return
		}
		sourceSnapshot = &snapshot
	}
	if sinkSnapshot == nil {
		snapshot, err := buildPipelineSnapshot(sink, sinkDS)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_pipeline", err.Error())
			return
		}
		sinkSnapshot = &snapshot
	}
	transforms, err := decodeTransforms(pipeline.TransformsJSON)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_pipeline", "Transform 配置无法解析")
		return
	}
	preview, err := transform.Validate(sourceSnapshot.Schema, transforms)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_transform", err.Error())
		return
	}
	if !connector.SchemasEqual(preview.OutputSchema, sinkSnapshot.Schema) {
		writeError(w, http.StatusUnprocessableEntity, "schema_mismatch", "Transform 输出字段与目标字段不一致")
		return
	}
	if err := connector.ValidateSource(sourceSnapshot.connectorConfig(sourceDS)); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_pipeline", "源配置不正确："+err.Error())
		return
	}
	if err := connector.ValidateSink(sinkSnapshot.connectorConfig(sinkDS)); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_pipeline", "目标配置不正确："+err.Error())
		return
	}
	sourceJSON, err := json.Marshal(sourceSnapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "序列化版本失败")
		return
	}
	sinkJSON, err := json.Marshal(sinkSnapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "序列化版本失败")
		return
	}
	sourceEndpointID := sourceSnapshot.EndpointID
	if sourceEndpointID == 0 {
		sourceEndpointID = source.ID
	}
	sinkEndpointID := sinkSnapshot.EndpointID
	if sinkEndpointID == 0 {
		sinkEndpointID = sink.ID
	}
	version := &store.PipelineVersion{
		PipelineID:              pipeline.ID,
		SourceEndpointID:        sourceEndpointID,
		SinkEndpointID:          sinkEndpointID,
		SourceDataSourceID:      sourceDS.ID,
		SinkDataSourceID:        sinkDS.ID,
		SourceName:              sourceSnapshot.Name,
		SinkName:                sinkSnapshot.Name,
		Summary:                 sourceSnapshot.Name + " → " + sinkSnapshot.Name,
		SourceJSON:              string(sourceJSON),
		SinkJSON:                string(sinkJSON),
		TransformsJSON:          pipeline.TransformsJSON,
		TransformLibraryVersion: transform.FunctionLibraryVersion,
	}
	if err := s.dataStore.CreatePipelineVersion(version); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "发布版本失败")
		return
	}
	response, err := toPipelineVersionResponse(*version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败")
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleListPipelineVersions(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if _, err := s.dataStore.GetPipeline(id); errors.Is(err, store.ErrPipelineNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Pipeline 不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	versions, err := s.dataStore.ListPipelineVersions(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败")
		return
	}
	response := make([]pipelineVersionResponse, 0, len(versions))
	for _, version := range versions {
		item, err := toPipelineVersionResponse(version)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败")
			return
		}
		response = append(response, item)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetPipelineVersion(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	version, err := s.dataStore.GetPipelineVersion(id)
	if errors.Is(err, store.ErrPipelineVersionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "版本不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败")
		return
	}
	response, err := toPipelineVersionResponse(version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleRunPipeline(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	pipeline, err := s.dataStore.GetPipeline(id)
	if errors.Is(err, store.ErrPipelineNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Pipeline 不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	version, err := s.dataStore.LatestPipelineVersion(id)
	if errors.Is(err, store.ErrPipelineVersionNotFound) {
		writeError(w, http.StatusConflict, "not_published", "Pipeline 尚未发布版本")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败")
		return
	}
	s.startPipelineRun(w, pipeline, version, store.PipelineRunTriggerManual)
}

func (s *Server) handleRunPipelineVersion(w http.ResponseWriter, r *http.Request) {
	versionID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	version, err := s.dataStore.GetPipelineVersion(versionID)
	if errors.Is(err, store.ErrPipelineVersionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "版本不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败")
		return
	}
	pipeline, err := s.dataStore.GetPipeline(version.PipelineID)
	if errors.Is(err, store.ErrPipelineNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Pipeline 不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	s.startPipelineRun(w, pipeline, version, store.PipelineRunTriggerManual)
}

func (s *Server) handleRestorePipelineVersion(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	versionID, err := pathID(r, "versionId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	pipeline, err := s.dataStore.GetPipeline(pipelineID)
	if errors.Is(err, store.ErrPipelineNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Pipeline 不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	version, err := s.dataStore.GetPipelineVersion(versionID)
	if errors.Is(err, store.ErrPipelineVersionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "版本不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败")
		return
	}
	if version.PipelineID != pipeline.ID {
		writeError(w, http.StatusBadRequest, "invalid_request", "版本不属于该 Pipeline")
		return
	}
	var source, sink pipelineEndpointSnapshot
	if err := json.Unmarshal([]byte(version.SourceJSON), &source); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本快照失败")
		return
	}
	if err := json.Unmarshal([]byte(version.SinkJSON), &sink); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本快照失败")
		return
	}
	sourceID := version.SourceEndpointID
	if sourceID == 0 {
		sourceID = source.EndpointID
	}
	sinkID := version.SinkEndpointID
	if sinkID == 0 {
		sinkID = sink.EndpointID
	}
	if _, err := s.dataStore.GetEndpoint(store.EndpointKindSource, sourceID); err != nil {
		writeError(w, http.StatusConflict, "endpoint_missing", "版本引用的源已不存在")
		return
	}
	if _, err := s.dataStore.GetEndpoint(store.EndpointKindSink, sinkID); err != nil {
		writeError(w, http.StatusConflict, "endpoint_missing", "版本引用的目标已不存在")
		return
	}
	pipeline.SourceID = sourceID
	pipeline.SinkID = sinkID
	pipeline.DraftSourceJSON = version.SourceJSON
	pipeline.DraftSinkJSON = version.SinkJSON
	pipeline.DraftVersionID = &version.ID
	pipeline.DraftVersionNumber = version.VersionNumber
	pipeline.TransformsJSON = version.TransformsJSON
	if err := s.dataStore.UpdatePipeline(&pipeline); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "更新草稿失败")
		return
	}
	response, err := s.loadPipelineResponse(pipeline)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCancelPipelineRun(w http.ResponseWriter, r *http.Request) {
	runID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	run, err := s.dataStore.GetPipelineRun(runID)
	if errors.Is(err, store.ErrPipelineRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "运行记录不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行记录失败")
		return
	}
	if run.Status != store.PipelineRunStatusRunning {
		writeJSON(w, http.StatusOK, toPipelineRunResponse(run))
		return
	}
	if run.PipelineID == nil {
		writeError(w, http.StatusConflict, "run_not_pipeline", "只有 Pipeline 运行可以取消")
		return
	}
	if s.worker == nil {
		writeError(w, http.StatusServiceUnavailable, "worker_unavailable", "etl-worker 未启用")
		return
	}
	cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = s.worker.CancelPipelineRun(cancelCtx, run.ID)
	cancel()
	if errors.Is(err, errPipelineRunNotActive) {
		writeError(w, http.StatusConflict, "run_not_active", "该运行已不在执行中")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "取消运行失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "canceling"})
}

func (s *Server) handleRerunPipelineRun(w http.ResponseWriter, r *http.Request) {
	runID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	previous, err := s.dataStore.GetPipelineRun(runID)
	if errors.Is(err, store.ErrPipelineRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "运行记录不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行记录失败")
		return
	}
	if previous.Status != store.PipelineRunStatusFailed {
		writeError(w, http.StatusConflict, "run_not_failed", "只有失败的运行可以重跑")
		return
	}
	if previous.PipelineID == nil || previous.VersionID == nil {
		writeError(w, http.StatusConflict, "run_not_replayable", "该运行缺少 Pipeline 或版本信息")
		return
	}
	pipeline, err := s.dataStore.GetPipeline(*previous.PipelineID)
	if errors.Is(err, store.ErrPipelineNotFound) {
		writeError(w, http.StatusConflict, "pipeline_deleted", "原 Pipeline 已删除")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	version, err := s.dataStore.GetPipelineVersion(*previous.VersionID)
	if errors.Is(err, store.ErrPipelineVersionNotFound) {
		writeError(w, http.StatusConflict, "version_deleted", "原版本已不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败")
		return
	}
	if version.PipelineID != pipeline.ID {
		writeError(w, http.StatusConflict, "version_mismatch", "原版本不属于该 Pipeline")
		return
	}
	s.startPipelineRun(w, pipeline, version, store.PipelineRunTriggerRerun)
}

func (s *Server) startPipelineRun(w http.ResponseWriter, pipeline store.Pipeline, version store.PipelineVersion, trigger string) {
	if s.worker == nil {
		writeError(w, http.StatusServiceUnavailable, "worker_unavailable", "etl-worker 未启用")
		return
	}
	runLock := s.pipelineRunLock(pipeline.ID)
	runLock.Lock()
	defer runLock.Unlock()
	active, err := s.dataStore.HasActivePipelineRun(pipeline.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "检查运行状态失败")
		return
	}
	if active {
		writeError(w, http.StatusConflict, "pipeline_running", "该 Pipeline 已有运行中的实例")
		return
	}
	var source, sink pipelineEndpointSnapshot
	if err := json.Unmarshal([]byte(version.SourceJSON), &source); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本快照失败")
		return
	}
	if err := json.Unmarshal([]byte(version.SinkJSON), &sink); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本快照失败")
		return
	}
	sourceDS, err := s.dataStore.GetDataSource(source.DataSourceID)
	if errors.Is(err, store.ErrDataSourceNotFound) {
		writeError(w, http.StatusConflict, "data_source_missing", "版本引用的源数据源已删除")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取源数据源失败")
		return
	}
	sinkDS, err := s.dataStore.GetDataSource(sink.DataSourceID)
	if errors.Is(err, store.ErrDataSourceNotFound) {
		writeError(w, http.StatusConflict, "data_source_missing", "版本引用的目标数据源已删除")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取目标数据源失败")
		return
	}
	transforms, err := decodeTransforms(version.TransformsJSON)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本 Transform 失败")
		return
	}
	spec := worker.RunSpec{Pipeline: &worker.PipelineSpec{
		Source:                  source.workerEndpoint(sourceDS),
		Transforms:              transforms,
		TransformLibraryVersion: version.TransformLibraryVersion,
		Sink:                    sink.workerEndpoint(sinkDS),
	}}

	pipelineID := pipeline.ID
	versionID := version.ID
	run := &store.PipelineRun{
		PipelineID:    &pipelineID,
		VersionID:     &versionID,
		VersionNumber: version.VersionNumber,
		PipelineName:  pipeline.Name,
		Status:        store.PipelineRunStatusRunning,
		Trigger:       trigger,
		StartedAt:     time.Now(),
	}
	if err := s.dataStore.CreatePipelineRun(run); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "创建运行记录失败")
		return
	}
	logDir := s.config.LogsDir()
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建日志目录失败")
		return
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("pipeline-run-%d.log", run.ID))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建日志文件失败")
		return
	}
	_ = logFile.Close()
	run.LogPath = logPath
	if err := s.dataStore.UpdatePipelineRunLogPath(run.ID, logPath); err != nil {
		_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "保存日志路径失败")
		return
	}

	s.pipelineStreams.start(run.ID)
	startCtx, cancelStart := context.WithTimeout(context.Background(), 10*time.Second)
	workerRunID, err := s.worker.StartTrackedRun(startCtx, spec, run.ID)
	cancelStart()
	if err != nil {
		s.pipelineStreams.finish(run.ID)
		_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, err.Error())
		writeError(w, http.StatusServiceUnavailable, "worker_unavailable", "启动 etl-worker 失败："+err.Error())
		return
	}
	go s.executePipelineRun(workerRunID, *run)
	response := toPipelineRunResponse(*run)
	writeJSON(w, http.StatusAccepted, response)
}

func parsePipelineRunFilter(r *http.Request) store.PipelineRunFilter {
	filter := store.PipelineRunFilter{Status: r.URL.Query().Get("status")}
	if value := r.URL.Query().Get("pipelineId"); value != "" {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			filter.PipelineID = id
		}
	}
	if value := r.URL.Query().Get("page"); value != "" {
		if page, err := strconv.Atoi(value); err == nil {
			filter.Page = page
		}
	}
	if value := r.URL.Query().Get("pageSize"); value != "" {
		if size, err := strconv.Atoi(value); err == nil {
			filter.PageSize = size
		}
	}
	return filter
}
