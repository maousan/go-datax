package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/connector"
	"github.com/longkeyy/go-datax/internal/store"
)

type endpointRequest struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Connector    string            `json:"connector"`
	DataSourceID int64             `json:"dataSourceId"`
	Table        string            `json:"table"`
	Query        string            `json:"query"`
	Path         string            `json:"path"`
	Schema       connector.Schema  `json:"schema"`
	Options      map[string]string `json:"options"`
}

type endpointResponse struct {
	ID             int64             `json:"id"`
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Connector      string            `json:"connector"`
	DataSourceID   int64             `json:"dataSourceId"`
	DataSourceName string            `json:"dataSourceName"`
	Table          string            `json:"table"`
	Query          string            `json:"query"`
	Path           string            `json:"path"`
	Schema         connector.Schema  `json:"schema"`
	Options        map[string]string `json:"options"`
	CreatedAt      string            `json:"createdAt"`
	UpdatedAt      string            `json:"updatedAt"`
}

func (s *Server) registerEndpointRoutes(mux *http.ServeMux, kind, base string) {
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		s.handleListEndpoints(kind, w, r)
	})
	mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		s.handleCreateEndpoint(kind, w, r)
	})
	mux.HandleFunc("POST "+base+"/discover", func(w http.ResponseWriter, r *http.Request) {
		s.handleDiscoverEndpoint(kind, w, r)
	})
	mux.HandleFunc("GET "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.handleGetEndpoint(kind, w, r)
	})
	mux.HandleFunc("PUT "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.handleUpdateEndpoint(kind, w, r)
	})
	mux.HandleFunc("DELETE "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.handleDeleteEndpoint(kind, w, r)
	})
	mux.HandleFunc(base, methodNotAllowed("GET, POST"))
	mux.HandleFunc("GET "+base+"/discover", methodNotAllowed(http.MethodPost))
	mux.HandleFunc("POST "+base+"/{id}", methodNotAllowed("GET, PUT, DELETE"))
}

func normalizeEndpointRequest(req endpointRequest, kind string, requireSchema bool) (endpointRequest, error) {
	if !store.ValidEndpointKind(kind) {
		return req, fmt.Errorf("无效的端点类型")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.Connector = strings.ToLower(strings.TrimSpace(req.Connector))
	req.Table = strings.TrimSpace(req.Table)
	req.Query = strings.TrimSpace(req.Query)
	req.Path = strings.TrimSpace(req.Path)

	if requireSchema {
		if req.Name == "" {
			return req, fmt.Errorf("名称不能为空")
		}
		if len([]rune(req.Name)) > 100 {
			return req, fmt.Errorf("名称不能超过 100 个字符")
		}
		if len([]rune(req.Description)) > 500 {
			return req, fmt.Errorf("说明不能超过 500 个字符")
		}
	}

	switch req.Connector {
	case "sqlite", "mysql", "postgres":
		if req.Path != "" {
			return req, fmt.Errorf("数据库连接器使用表或查询，不接受文件路径")
		}
		if len([]rune(req.Table)) > 200 {
			return req, fmt.Errorf("表名不能超过 200 个字符")
		}
		if len([]rune(req.Query)) > 20000 {
			return req, fmt.Errorf("查询不能超过 20000 个字符")
		}
	case "csv", "txt", "jsonl":
		if req.Table != "" || req.Query != "" {
			return req, fmt.Errorf("文件连接器使用文件路径，不接受表或查询")
		}
		if req.Path == "" {
			return req, fmt.Errorf("文件路径不能为空")
		}
		if len([]rune(req.Path)) > 1000 {
			return req, fmt.Errorf("文件路径不能超过 1000 个字符")
		}
	default:
		return req, fmt.Errorf("不支持的连接器：%s", req.Connector)
	}

	if kind == store.EndpointKindSink {
		if isDatabaseConnector(req.Connector) {
			if req.Query != "" {
				return req, fmt.Errorf("目标不支持查询")
			}
			if req.Table == "" {
				return req, fmt.Errorf("目标表不能为空")
			}
		}
	} else if isDatabaseConnector(req.Connector) && req.Table == "" && req.Query == "" {
		return req, fmt.Errorf("源必须填写表或查询")
	}

	options := map[string]string{}
	for key, value := range req.Options {
		key = strings.TrimSpace(key)
		if key == "" {
			return req, fmt.Errorf("选项名不能为空")
		}
		switch key {
		case "table", "query", "path":
			return req, fmt.Errorf("请使用 table、query、path 字段，而不是选项 %s", key)
		}
		options[key] = value
	}
	if kind == store.EndpointKindSource {
		if strings.TrimSpace(options["writeMode"]) != "" {
			return req, fmt.Errorf("写入模式仅用于目标")
		}
	} else if strings.TrimSpace(options["splitPk"]) != "" {
		return req, fmt.Errorf("分片字段仅用于源")
	}
	req.Options = options

	if requireSchema {
		if len(req.Schema) == 0 {
			return req, fmt.Errorf("字段不能为空")
		}
		for index := range req.Schema {
			req.Schema[index].Name = strings.TrimSpace(req.Schema[index].Name)
		}
		if err := req.Schema.Validate(); err != nil {
			return req, fmt.Errorf("字段定义不正确：%w", err)
		}
	}
	return req, nil
}

func isDatabaseConnector(name string) bool {
	switch name {
	case "sqlite", "mysql", "postgres":
		return true
	default:
		return false
	}
}

func endpointConnectorConfig(ds store.DataSource, req endpointRequest, schema connector.Schema) connector.Config {
	return buildConnectorConfig(ds, req.Connector, schema, req.Options, req.Table, req.Query, req.Path)
}

func buildConnectorConfig(ds store.DataSource, connectorName string, schema connector.Schema, sourceOptions map[string]string, table, query, path string) connector.Config {
	properties := map[string]string{}
	for key, value := range ds.Properties {
		properties[key] = value
	}
	options := map[string]string{}
	for key, value := range sourceOptions {
		options[key] = value
	}
	if table != "" {
		options["table"] = table
	}
	if query != "" {
		options["query"] = query
	}
	if path != "" {
		options["path"] = path
	}
	return connector.Config{
		Connector:            connectorName,
		DataSourceType:       ds.Type,
		JDBCUrl:              ds.JDBCUrl,
		Username:             ds.Username,
		Password:             ds.Password,
		DataSourceProperties: properties,
		Schema:               schema,
		Options:              options,
	}
}

func validateEndpointDefinition(kind string, cfg connector.Config) error {
	if kind == store.EndpointKindSource {
		return connector.ValidateSource(cfg)
	}
	return connector.ValidateSink(cfg)
}

func (s *Server) handleListEndpoints(kind string, w http.ResponseWriter, r *http.Request) {
	items, err := s.dataStore.ListEndpoints(kind, r.URL.Query().Get("query"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取源与目标失败")
		return
	}
	dataSources, err := s.dataStore.ListDataSources("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
		return
	}
	names := make(map[int64]string, len(dataSources))
	for _, ds := range dataSources {
		names[ds.ID] = ds.Name
	}
	response := make([]endpointResponse, 0, len(items))
	for _, item := range items {
		converted, err := toEndpointResponse(item, names[item.DataSourceID])
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取源与目标失败")
			return
		}
		response = append(response, converted)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetEndpoint(kind string, w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	endpoint, err := s.dataStore.GetEndpoint(kind, id)
	if errors.Is(err, store.ErrEndpointNotFound) {
		writeError(w, http.StatusNotFound, "not_found", endpointNotFoundMessage(kind))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取源与目标失败")
		return
	}
	ds, err := s.dataStore.GetDataSource(endpoint.DataSourceID)
	if err != nil && !errors.Is(err, store.ErrDataSourceNotFound) {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
		return
	}
	converted, err := toEndpointResponse(endpoint, ds.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取源与目标失败")
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (s *Server) handleCreateEndpoint(kind string, w http.ResponseWriter, r *http.Request) {
	var req endpointRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := normalizeEndpointRequest(req, kind, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ds, ok := s.endpointDataSource(w, normalized.DataSourceID)
	if !ok {
		return
	}
	cfg := endpointConnectorConfig(ds, normalized, normalized.Schema)
	if err := validateEndpointDefinition(kind, cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_connector_config", "连接器配置不正确："+err.Error())
		return
	}
	schemaJSON, err := json.Marshal(normalized.Schema)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "保存字段失败")
		return
	}
	endpoint := &store.Endpoint{
		Kind:         kind,
		Name:         normalized.Name,
		Description:  normalized.Description,
		Connector:    normalized.Connector,
		DataSourceID: normalized.DataSourceID,
		TableName:    normalized.Table,
		QueryText:    normalized.Query,
		Path:         normalized.Path,
		SchemaJSON:   string(schemaJSON),
		Options:      store.StringMap(normalized.Options),
	}
	if err := s.dataStore.CreateEndpoint(endpoint); err != nil {
		if errors.Is(err, store.ErrDuplicateEndpointName) {
			writeError(w, http.StatusConflict, "duplicate_name", endpointNameConflictMessage(kind))
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "创建源与目标失败")
		return
	}
	converted, err := toEndpointResponse(*endpoint, ds.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取源与目标失败")
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (s *Server) handleUpdateEndpoint(kind string, w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	existing, err := s.dataStore.GetEndpoint(kind, id)
	if errors.Is(err, store.ErrEndpointNotFound) {
		writeError(w, http.StatusNotFound, "not_found", endpointNotFoundMessage(kind))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取源与目标失败")
		return
	}
	var req endpointRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := normalizeEndpointRequest(req, kind, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ds, ok := s.endpointDataSource(w, normalized.DataSourceID)
	if !ok {
		return
	}
	cfg := endpointConnectorConfig(ds, normalized, normalized.Schema)
	if err := validateEndpointDefinition(kind, cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_connector_config", "连接器配置不正确："+err.Error())
		return
	}
	schemaJSON, err := json.Marshal(normalized.Schema)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "保存字段失败")
		return
	}
	existing.Name = normalized.Name
	existing.Description = normalized.Description
	existing.Connector = normalized.Connector
	existing.DataSourceID = normalized.DataSourceID
	existing.TableName = normalized.Table
	existing.QueryText = normalized.Query
	existing.Path = normalized.Path
	existing.SchemaJSON = string(schemaJSON)
	existing.Options = store.StringMap(normalized.Options)
	if err := s.dataStore.UpdateEndpoint(&existing); err != nil {
		if errors.Is(err, store.ErrDuplicateEndpointName) {
			writeError(w, http.StatusConflict, "duplicate_name", endpointNameConflictMessage(kind))
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "更新源与目标失败")
		return
	}
	converted, err := toEndpointResponse(existing, ds.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取源与目标失败")
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (s *Server) handleDeleteEndpoint(kind string, w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// Pipeline versions snapshot these definitions; only drafts block deletion.
	pipelines, err := s.dataStore.PipelinesUsingEndpoint(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "检查 Pipeline 引用失败")
		return
	}
	if len(pipelines) > 0 {
		names := make([]string, 0, len(pipelines))
		for _, pipeline := range pipelines {
			names = append(names, pipeline.Name)
		}
		writeError(w, http.StatusConflict, "endpoint_in_use",
			"该配置被以下 Pipeline 草稿引用："+strings.Join(names, "、"))
		return
	}
	versions, err := s.dataStore.PipelineVersionsUsingEndpoint(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "检查已发布版本引用失败")
		return
	}
	if len(versions) > 0 {
		writeError(w, http.StatusConflict, "endpoint_in_use",
			"该配置被已发布的 Pipeline 版本引用，不能删除")
		return
	}
	if err := s.dataStore.DeleteEndpoint(kind, id); err != nil {
		if errors.Is(err, store.ErrEndpointNotFound) {
			writeError(w, http.StatusNotFound, "not_found", endpointNotFoundMessage(kind))
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "删除源与目标失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDiscoverEndpoint(kind string, w http.ResponseWriter, r *http.Request) {
	var req endpointRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := normalizeEndpointRequest(req, kind, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ds, ok := s.endpointDataSource(w, normalized.DataSourceID)
	if !ok {
		return
	}
	cfg := endpointConnectorConfig(ds, normalized, nil)
	schema, err := connector.Discover(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_connector_config", "发现字段失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema": schema})
}

func (s *Server) endpointDataSource(w http.ResponseWriter, id int64) (store.DataSource, bool) {
	ds, err := s.dataStore.GetDataSource(id)
	if errors.Is(err, store.ErrDataSourceNotFound) {
		writeError(w, http.StatusBadRequest, "invalid_request", "数据源不存在")
		return ds, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
		return ds, false
	}
	return ds, true
}

func toEndpointResponse(endpoint store.Endpoint, dataSourceName string) (endpointResponse, error) {
	schema := connector.Schema{}
	if strings.TrimSpace(endpoint.SchemaJSON) != "" {
		if err := json.Unmarshal([]byte(endpoint.SchemaJSON), &schema); err != nil {
			return endpointResponse{}, err
		}
	}
	options := map[string]string{}
	for key, value := range endpoint.Options {
		options[key] = value
	}
	return endpointResponse{
		ID:             endpoint.ID,
		Kind:           endpoint.Kind,
		Name:           endpoint.Name,
		Description:    endpoint.Description,
		Connector:      endpoint.Connector,
		DataSourceID:   endpoint.DataSourceID,
		DataSourceName: dataSourceName,
		Table:          endpoint.TableName,
		Query:          endpoint.QueryText,
		Path:           endpoint.Path,
		Schema:         schema,
		Options:        options,
		CreatedAt:      endpoint.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      endpoint.UpdatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func endpointNotFoundMessage(kind string) string {
	if kind == store.EndpointKindSource {
		return "源不存在"
	}
	return "目标不存在"
}

func endpointNameConflictMessage(kind string) string {
	if kind == store.EndpointKindSource {
		return "源名称已存在"
	}
	return "目标名称已存在"
}
