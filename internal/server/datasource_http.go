package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/connectiontest"
	"github.com/longkeyy/go-datax/internal/store"
)

const (
	maskedPassword           = "******"
	dataSourceTypeFileSystem = "filesystem"
)

var supportedDataSourceTypes = map[string]bool{
	"mysql":                  true,
	"postgres":               true,
	"sqlite":                 true,
	"mssql":                  true,
	"oracle":                 true,
	dataSourceTypeFileSystem: true,
}

type dataSourceRequest struct {
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	JDBCUrl    string             `json:"jdbcUrl"`
	Username   string             `json:"username"`
	Password   string             `json:"password"`
	Properties *map[string]string `json:"properties"`
}

type dataSourceResponse struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	JDBCUrl    string            `json:"jdbcUrl"`
	Username   string            `json:"username"`
	Password   string            `json:"password"`
	Version    string            `json:"version"`
	Properties map[string]string `json:"properties"`
	CreatedAt  string            `json:"createdAt"`
	UpdatedAt  string            `json:"updatedAt"`
}

func (s *Server) handleListDataSources(w http.ResponseWriter, r *http.Request) {
	items, err := s.dataStore.ListDataSources(r.URL.Query().Get("query"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
		return
	}

	response := make([]dataSourceResponse, 0, len(items))
	for _, item := range items {
		response = append(response, toDataSourceResponse(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetDataSource(w http.ResponseWriter, r *http.Request) {
	id, err := dataSourceID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	ds, err := s.dataStore.GetDataSource(id)
	if errors.Is(err, store.ErrDataSourceNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "数据源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
		return
	}
	writeJSON(w, http.StatusOK, toDataSourceResponse(ds))
}

func (s *Server) handleCreateDataSource(w http.ResponseWriter, r *http.Request) {
	var req dataSourceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}

	validated, err := validateDataSourceRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	properties := map[string]string{}
	if validated.Properties != nil {
		properties = *validated.Properties
	}

	ds := &store.DataSource{
		Name:       validated.Name,
		Type:       validated.Type,
		JDBCUrl:    validated.JDBCUrl,
		Username:   validated.Username,
		Password:   validated.Password,
		Properties: store.StringMap(properties),
	}
	if err := s.dataStore.CreateDataSource(ds); err != nil {
		if errors.Is(err, store.ErrDuplicateDataSourceName) {
			writeError(w, http.StatusConflict, "duplicate_name", "数据源名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "创建数据源失败")
		return
	}
	writeJSON(w, http.StatusCreated, toDataSourceResponse(*ds))
}

func (s *Server) handleUpdateDataSource(w http.ResponseWriter, r *http.Request) {
	id, err := dataSourceID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	existing, err := s.dataStore.GetDataSource(id)
	if errors.Is(err, store.ErrDataSourceNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "数据源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
		return
	}

	var req dataSourceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}

	merged, err := mergeDataSource(existing, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if merged.Type != existing.Type {
		endpoints, err := s.dataStore.EndpointsUsingDataSource(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "检查源与目标引用失败")
			return
		}
		if len(endpoints) > 0 {
			writeError(w, http.StatusConflict, "data_source_in_use",
				"该数据源被 ETL 源或目标引用，不能修改类型")
			return
		}
		versions, err := s.dataStore.PipelineVersionsUsingDataSource(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "检查 Pipeline 版本引用失败")
			return
		}
		if len(versions) > 0 {
			writeError(w, http.StatusConflict, "data_source_in_use",
				"该数据源被已发布的 Pipeline 版本引用，不能修改类型")
			return
		}
	}
	if err := s.dataStore.UpdateDataSource(&merged); err != nil {
		if errors.Is(err, store.ErrDuplicateDataSourceName) {
			writeError(w, http.StatusConflict, "duplicate_name", "数据源名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "更新数据源失败")
		return
	}
	writeJSON(w, http.StatusOK, toDataSourceResponse(merged))
}

func (s *Server) handleDeleteDataSource(w http.ResponseWriter, r *http.Request) {
	id, err := dataSourceID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	names, err := s.jobNamesReferencingDataSource(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "检查数据源引用失败")
		return
	}
	templates, err := s.dataStore.QueryTemplatesUsingDataSource(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "检查数据集引用失败")
		return
	}
	endpoints, err := s.dataStore.EndpointsUsingDataSource(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "检查源与目标引用失败")
		return
	}
	versions, err := s.dataStore.PipelineVersionsUsingDataSource(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "检查 Pipeline 版本引用失败")
		return
	}
	var groups []string
	if len(names) > 0 || len(templates) > 0 {
		var dataxParts []string
		if len(names) > 0 {
			dataxParts = append(dataxParts, "datax任务："+strings.Join(names, "、"))
		}
		if len(templates) > 0 {
			templateNames := make([]string, 0, len(templates))
			for _, template := range templates {
				templateNames = append(templateNames, template.Name)
			}
			dataxParts = append(dataxParts, "数据集："+strings.Join(templateNames, "、"))
		}
		groups = append(groups, "DataX 区："+strings.Join(dataxParts, "；"))
	}
	if len(endpoints) > 0 {
		var sources, sinks []string
		for _, endpoint := range endpoints {
			if endpoint.Kind == store.EndpointKindSource {
				sources = append(sources, endpoint.Name)
			} else {
				sinks = append(sinks, endpoint.Name)
			}
		}
		var etlParts []string
		if len(sources) > 0 {
			etlParts = append(etlParts, "源："+strings.Join(sources, "、"))
		}
		if len(sinks) > 0 {
			etlParts = append(etlParts, "目标："+strings.Join(sinks, "、"))
		}
		groups = append(groups, "ETL 区："+strings.Join(etlParts, "；"))
	}
	if len(versions) > 0 {
		groups = append(groups, fmt.Sprintf("Pipeline 区：%d 个已发布版本", len(versions)))
	}
	if len(groups) > 0 {
		// Pipeline 版本保存 Source/Sink 快照，不阻塞删除。
		writeError(w, http.StatusConflict, "data_source_in_use",
			"该数据源被以下配置引用："+strings.Join(groups, "；"))
		return
	}

	if err := s.dataStore.DeleteDataSource(id); err != nil {
		if errors.Is(err, store.ErrDataSourceNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "数据源不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "删除数据源失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mergeDataSource 应用更新请求；空密码或掩码表示保留原密码。
func mergeDataSource(existing store.DataSource, req dataSourceRequest) (store.DataSource, error) {
	validated, err := validateDataSourceRequest(req)
	if err != nil {
		return existing, err
	}

	if validated.Type != existing.Type {
		existing.Version = ""
		existing.Properties = store.StringMap{}
	}
	existing.Name = validated.Name
	existing.Type = validated.Type
	existing.JDBCUrl = validated.JDBCUrl
	existing.Username = validated.Username
	if validated.Type == dataSourceTypeFileSystem {
		existing.Password = ""
	} else if validated.Password != "" && validated.Password != maskedPassword {
		existing.Password = validated.Password
	}
	if validated.Properties != nil {
		existing.Properties = store.StringMap(*validated.Properties)
	}
	return existing, nil
}

func validateDataSourceRequest(req dataSourceRequest) (dataSourceRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	req.JDBCUrl = strings.TrimSpace(req.JDBCUrl)
	req.Username = strings.TrimSpace(req.Username)

	if req.Name == "" {
		return req, fmt.Errorf("名称不能为空")
	}
	if len([]rune(req.Name)) > 100 {
		return req, fmt.Errorf("名称不能超过 100 个字符")
	}

	if !supportedDataSourceTypes[req.Type] {
		return req, fmt.Errorf("类型必须是 mysql、postgres、sqlite、mssql、oracle 或 filesystem")
	}
	if req.Type == dataSourceTypeFileSystem {
		properties := map[string]string{}
		if req.Properties != nil {
			properties = *req.Properties
		}
		basePath := strings.TrimSpace(properties["basePath"])
		if basePath == "" {
			return req, fmt.Errorf("根目录不能为空")
		}
		if !filepath.IsAbs(basePath) {
			return req, fmt.Errorf("根目录必须是绝对路径")
		}
		properties["basePath"] = filepath.Clean(basePath)
		req.Properties = &properties
		req.JDBCUrl = ""
		req.Username = ""
		req.Password = ""
		return req, nil
	}
	if req.JDBCUrl == "" {
		var properties map[string]string
		if req.Properties != nil {
			properties = *req.Properties
		}
		req.JDBCUrl = composeJDBCURL(req.Type, properties)
	}
	if req.JDBCUrl == "" {
		return req, fmt.Errorf("连接地址不能为空，或提供 host/port/database 等 connector 配置")
	}
	if !validJDBCURL(req.Type, req.JDBCUrl) {
		return req, fmt.Errorf("%s 类型的连接地址格式不正确", req.Type)
	}
	if jdbcURLHasCredentials(req.Type, req.JDBCUrl) {
		return req, fmt.Errorf("连接地址中不能包含用户名或密码，请使用独立字段")
	}
	if req.Type != "sqlite" && req.Username == "" {
		return req, fmt.Errorf("用户名不能为空")
	}

	return req, nil
}

func toDataSourceResponse(ds store.DataSource) dataSourceResponse {
	password := maskedPassword
	if ds.Type == dataSourceTypeFileSystem {
		password = ""
	}
	return dataSourceResponse{
		ID:         ds.ID,
		Name:       ds.Name,
		Type:       ds.Type,
		JDBCUrl:    ds.JDBCUrl,
		Username:   ds.Username,
		Password:   password,
		Version:    ds.Version,
		Properties: ds.Properties,
		CreatedAt:  ds.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  ds.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// composeJDBCURL builds a JDBC URL from connector properties when jdbcUrl is omitted.
func composeJDBCURL(dsType string, properties map[string]string) string {
	if properties == nil {
		return ""
	}
	value := func(keys ...string) string {
		for _, key := range keys {
			if raw := strings.TrimSpace(properties[key]); raw != "" {
				return raw
			}
		}
		return ""
	}

	host := value("host")
	port := value("port")
	database := value("database")

	if host != "" && strings.ContainsAny(host, "/?#;@ \t") {
		return ""
	}
	if port != "" {
		parsed, err := strconv.Atoi(port)
		if err != nil || parsed < 1 || parsed > 65535 {
			return ""
		}
	}
	if !safeJDBCSegment(database) {
		return ""
	}

	switch dsType {
	case "sqlite":
		path := value("path", "database")
		if path == "" {
			return ""
		}
		return "jdbc:sqlite:" + path
	case "mysql":
		if host == "" {
			return ""
		}
		if port == "" {
			port = "3306"
		}
		conn := "jdbc:mysql://" + net.JoinHostPort(host, port)
		if database != "" {
			conn += "/" + database
		}
		return conn
	case "postgres":
		if host == "" {
			return ""
		}
		if port == "" {
			port = "5432"
		}
		conn := "jdbc:postgresql://" + net.JoinHostPort(host, port)
		if database != "" {
			conn += "/" + database
		}
		return conn
	case "mssql":
		if host == "" {
			return ""
		}
		if port == "" {
			port = "1433"
		}
		conn := "jdbc:sqlserver://" + net.JoinHostPort(host, port)
		if database != "" {
			conn += ";DatabaseName=" + database
		}
		return conn
	case "oracle":
		if host == "" {
			return ""
		}
		if port == "" {
			port = "1521"
		}
		service := value("service", "database")
		if service == "" || !safeJDBCSegment(service) {
			return ""
		}
		return "jdbc:oracle:thin:@//" + net.JoinHostPort(host, port) + "/" + service
	default:
		return ""
	}
}

func safeJDBCSegment(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.', r == '$':
		default:
			return false
		}
	}
	return true
}

func validJDBCURL(dsType, jdbcURL string) bool {
	lower := strings.ToLower(jdbcURL)
	switch dsType {
	case "mysql":
		return strings.HasPrefix(lower, "jdbc:mysql://")
	case "postgres":
		return strings.HasPrefix(lower, "jdbc:postgresql://")
	case "sqlite":
		return strings.HasPrefix(lower, "jdbc:sqlite:")
	case "mssql":
		return strings.HasPrefix(lower, "jdbc:sqlserver://") ||
			strings.HasPrefix(lower, "jdbc:jtds:sqlserver://")
	case "oracle":
		return strings.HasPrefix(lower, "jdbc:oracle:thin:@") ||
			strings.HasPrefix(lower, "oracle://")
	default:
		return false
	}
}

// jdbcURLHasCredentials 拒绝把凭据写进连接地址，避免脱敏逻辑被绕过。
func jdbcURLHasCredentials(dsType, jdbcURL string) bool {
	lower := strings.ToLower(jdbcURL)

	switch dsType {
	case "mysql", "postgres":
		prefix := "jdbc:mysql://"
		if dsType == "postgres" {
			prefix = "jdbc:postgresql://"
		}
		if !strings.HasPrefix(lower, prefix) {
			return false
		}
		parsed, err := url.Parse("scheme://" + jdbcURL[len(prefix):])
		return err == nil && parsed.User != nil

	case "mssql":
		prefix := ""
		switch {
		case strings.HasPrefix(lower, "jdbc:sqlserver://"):
			prefix = "jdbc:sqlserver://"
		case strings.HasPrefix(lower, "jdbc:jtds:sqlserver://"):
			prefix = "jdbc:jtds:sqlserver://"
		default:
			return false
		}
		hostPart := strings.SplitN(jdbcURL[len(prefix):], ";", 2)[0]
		return strings.Contains(hostPart, "@")

	case "oracle":
		lower := strings.ToLower(jdbcURL)
		var rest string
		switch {
		case strings.HasPrefix(lower, "jdbc:oracle:thin:@"):
			rest = jdbcURL[len("jdbc:oracle:thin:@"):]
		case strings.HasPrefix(lower, "oracle://"):
			rest = jdbcURL[len("oracle://"):]
		default:
			return false
		}
		return strings.Contains(rest, "@")

	default:
		return false
	}
}

type dataSourceTestRequest struct {
	ID int64 `json:"id"`
	dataSourceRequest
}

func (s *Server) handleTestDataSource(w http.ResponseWriter, r *http.Request) {
	var req dataSourceTestRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}

	dsType := strings.ToLower(strings.TrimSpace(req.Type))
	var saved store.DataSource
	hasSaved := false
	if req.ID > 0 {
		var err error
		saved, err = s.dataStore.GetDataSource(req.ID)
		if errors.Is(err, store.ErrDataSourceNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "数据源不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
			return
		}
		hasSaved = true
		if dsType == "" {
			dsType = saved.Type
		}
	}
	if dsType == dataSourceTypeFileSystem {
		properties := map[string]string{}
		if req.Properties != nil {
			properties = *req.Properties
		}
		basePath := strings.TrimSpace(properties["basePath"])
		if basePath == "" && hasSaved {
			basePath = strings.TrimSpace(saved.Properties["basePath"])
		}
		if basePath == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "根目录不能为空")
			return
		}
		if !filepath.IsAbs(basePath) {
			writeError(w, http.StatusBadRequest, "invalid_request", "根目录必须是绝对路径")
			return
		}
		result := connectiontest.TestFileSystem(basePath)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        result.OK,
			"latencyMs": result.LatencyMs,
			"message":   result.Message,
			"version":   result.Version,
		})
		return
	}

	var params connectiontest.Params
	if hasSaved {
		params = connectiontest.Params{
			Type:     saved.Type,
			JDBCUrl:  saved.JDBCUrl,
			Username: saved.Username,
			Password: saved.Password,
		}

		// 编辑中的表单可能未保存，允许用提交字段覆盖已保存的连接信息；
		// 空密码表示沿用已保存的密码，便于“编辑后直接测试连接”。
		if value := strings.ToLower(strings.TrimSpace(req.Type)); value != "" {
			params.Type = value
		}
		if value := strings.TrimSpace(req.JDBCUrl); value != "" {
			params.JDBCUrl = value
		} else if req.Properties != nil {
			if composed := composeJDBCURL(params.Type, *req.Properties); composed != "" {
				params.JDBCUrl = composed
			}
		}
		if value := strings.TrimSpace(req.Username); value != "" {
			params.Username = value
		}
		if req.Password != "" && req.Password != maskedPassword {
			params.Password = req.Password
		}

		if !supportedDataSourceTypes[params.Type] || !validJDBCURL(params.Type, params.JDBCUrl) {
			writeError(w, http.StatusBadRequest, "invalid_request", "连接参数格式不正确")
			return
		}
		if jdbcURLHasCredentials(params.Type, params.JDBCUrl) {
			writeError(w, http.StatusBadRequest, "invalid_request", "连接地址中不能包含用户名或密码")
			return
		}
	} else {
		validated, err := validateDataSourceRequest(req.dataSourceRequest)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		params = connectiontest.Params{
			Type:     validated.Type,
			JDBCUrl:  validated.JDBCUrl,
			Username: validated.Username,
			Password: validated.Password,
		}
	}

	result := connectiontest.Test(r.Context(), params)
	if result.OK && req.ID > 0 && result.Version != "" {
		// 只刷新版本号元数据，不改变 updated_at。
		_ = s.dataStore.UpdateDataSourceVersion(req.ID, result.Version)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        result.OK,
		"latencyMs": result.LatencyMs,
		"message":   result.Message,
		"version":   result.Version,
	})
}

func dataSourceID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("无效的数据源 ID")
	}
	return id, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	return decoder.Decode(target)
}
