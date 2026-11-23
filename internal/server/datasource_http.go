package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/connectiontest"
	"github.com/longkeyy/go-datax/internal/store"
)

const maskedPassword = "******"

var supportedDataSourceTypes = map[string]bool{
	"mysql":    true,
	"postgres": true,
	"sqlite":   true,
	"mssql":    true,
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
	if len(names) > 0 {
		writeError(w, http.StatusConflict, "data_source_in_use",
			"该数据源被以下同步任务引用："+strings.Join(names, "、"))
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

	existing.Name = validated.Name
	existing.Type = validated.Type
	existing.JDBCUrl = validated.JDBCUrl
	existing.Username = validated.Username
	if validated.Password != "" && validated.Password != maskedPassword {
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
		return req, fmt.Errorf("类型必须是 mysql、postgres、sqlite 或 mssql")
	}
	if req.JDBCUrl == "" {
		return req, fmt.Errorf("连接地址不能为空")
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
	return dataSourceResponse{
		ID:         ds.ID,
		Name:       ds.Name,
		Type:       ds.Type,
		JDBCUrl:    ds.JDBCUrl,
		Username:   ds.Username,
		Password:   maskedPassword,
		Properties: ds.Properties,
		CreatedAt:  ds.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  ds.UpdatedAt.UTC().Format(time.RFC3339),
	}
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

	var params connectiontest.Params
	if req.ID > 0 {
		ds, err := s.dataStore.GetDataSource(req.ID)
		if errors.Is(err, store.ErrDataSourceNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "数据源不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
			return
		}
		params = connectiontest.Params{
			Type:     ds.Type,
			JDBCUrl:  ds.JDBCUrl,
			Username: ds.Username,
			Password: ds.Password,
		}

		// 编辑中的表单可能未保存，允许用提交字段覆盖已保存的连接信息；
		// 空密码表示沿用已保存的密码，便于“编辑后直接测试连接”。
		if value := strings.ToLower(strings.TrimSpace(req.Type)); value != "" {
			params.Type = value
		}
		if value := strings.TrimSpace(req.JDBCUrl); value != "" {
			params.JDBCUrl = value
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
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        result.OK,
		"latencyMs": result.LatencyMs,
		"message":   result.Message,
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
