package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

type queryTemplateRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	DataSourceID int64    `json:"dataSourceId"`
	SQL          string   `json:"sql"`
	Table        string   `json:"table"`
	Columns      []string `json:"columns"`
	Where        string   `json:"where"`
	SplitPk      string   `json:"splitPk"`
}

type queryTemplateResponse struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	DataSourceID   int64    `json:"dataSourceId"`
	DataSourceName string   `json:"dataSourceName"`
	SQL            string   `json:"sql"`
	Table          string   `json:"table"`
	Columns        []string `json:"columns"`
	Where          string   `json:"where"`
	SplitPk        string   `json:"splitPk"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
}

func (req queryTemplateRequest) normalized() (queryTemplateRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, fmt.Errorf("名称不能为空")
	}
	if len([]rune(req.Name)) > 100 {
		return req, fmt.Errorf("名称不能超过 100 个字符")
	}
	req.Description = strings.TrimSpace(req.Description)
	if len([]rune(req.Description)) > 500 {
		return req, fmt.Errorf("说明不能超过 500 个字符")
	}
	if req.DataSourceID <= 0 {
		return req, fmt.Errorf("必须选择数据源")
	}
	columns, err := normalizeTemplateColumns(req.Columns)
	if err != nil {
		return req, err
	}
	req.Columns = columns

	req.SQL = strings.TrimSpace(req.SQL)
	if req.SQL != "" {
		normalizedSQL, err := normalizeTemplateSQL(req.SQL)
		if err != nil {
			return req, err
		}
		req.SQL = normalizedSQL
		// SQL 数据集由 querySql 执行，表/条件/分片字段不再参与执行。
		req.Table = ""
		req.Where = ""
		req.SplitPk = ""
		return req, nil
	}

	req.Table = strings.TrimSpace(req.Table)
	if req.Table == "" {
		return req, fmt.Errorf("表名不能为空")
	}
	if len([]rune(req.Table)) > 200 {
		return req, fmt.Errorf("表名不能超过 200 个字符")
	}
	req.Where = strings.TrimSpace(req.Where)
	if len([]rune(req.Where)) > 2000 {
		return req, fmt.Errorf("WHERE 条件不能超过 2000 个字符")
	}
	req.SplitPk = strings.TrimSpace(req.SplitPk)
	if len([]rune(req.SplitPk)) > 100 {
		return req, fmt.Errorf("分片字段不能超过 100 个字符")
	}
	return req, nil
}

func normalizeTemplateColumns(columns []string) ([]string, error) {
	result := make([]string, 0, len(columns))
	seen := map[string]bool{}
	for _, column := range columns {
		column = strings.TrimSpace(column)
		if column == "" {
			continue
		}
		key := strings.ToLower(column)
		if seen[key] {
			return nil, fmt.Errorf("列 %s 重复", column)
		}
		seen[key] = true
		result = append(result, column)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("列不能为空")
	}
	return result, nil
}

func (s *Server) handleListQueryTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.dataStore.ListQueryTemplates(r.URL.Query().Get("query"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据集失败")
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
	response := make([]queryTemplateResponse, 0, len(templates))
	for _, template := range templates {
		response = append(response, toQueryTemplateResponse(template, names[template.DataSourceID]))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetQueryTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	template, err := s.dataStore.GetQueryTemplate(id)
	if errors.Is(err, store.ErrQueryTemplateNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "数据集不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据集失败")
		return
	}
	ds, _ := s.dataStore.GetDataSource(template.DataSourceID)
	writeJSON(w, http.StatusOK, toQueryTemplateResponse(template, ds.Name))
}

func (s *Server) handleCreateQueryTemplate(w http.ResponseWriter, r *http.Request) {
	var req queryTemplateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := req.normalized()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ds, err := s.dataStore.GetDataSource(normalized.DataSourceID)
	if errors.Is(err, store.ErrDataSourceNotFound) {
		writeError(w, http.StatusBadRequest, "invalid_request", "数据源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
		return
	}

	template := &store.QueryTemplate{
		Name:         normalized.Name,
		Description:  normalized.Description,
		DataSourceID: normalized.DataSourceID,
		TableName:    normalized.Table,
		Columns:      store.EncodeQueryColumns(normalized.Columns),
		WhereClause:  normalized.Where,
		SplitPk:      normalized.SplitPk,
		SQLText:      normalized.SQL,
	}
	if err := s.dataStore.CreateQueryTemplate(template); err != nil {
		if errors.Is(err, store.ErrDuplicateQueryTemplateName) {
			writeError(w, http.StatusConflict, "duplicate_name", "数据集名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "创建数据集失败")
		return
	}
	writeJSON(w, http.StatusCreated, toQueryTemplateResponse(*template, ds.Name))
}

func (s *Server) handleUpdateQueryTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	existing, err := s.dataStore.GetQueryTemplate(id)
	if errors.Is(err, store.ErrQueryTemplateNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "数据集不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据集失败")
		return
	}

	var req queryTemplateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := req.normalized()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ds, err := s.dataStore.GetDataSource(normalized.DataSourceID)
	if errors.Is(err, store.ErrDataSourceNotFound) {
		writeError(w, http.StatusBadRequest, "invalid_request", "数据源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取数据源失败")
		return
	}

	existing.Name = normalized.Name
	existing.Description = normalized.Description
	existing.DataSourceID = normalized.DataSourceID
	existing.TableName = normalized.Table
	existing.Columns = store.EncodeQueryColumns(normalized.Columns)
	existing.WhereClause = normalized.Where
	existing.SplitPk = normalized.SplitPk
	existing.SQLText = normalized.SQL
	if err := s.dataStore.UpdateQueryTemplate(&existing); err != nil {
		if errors.Is(err, store.ErrDuplicateQueryTemplateName) {
			writeError(w, http.StatusConflict, "duplicate_name", "数据集名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "更新数据集失败")
		return
	}
	writeJSON(w, http.StatusOK, toQueryTemplateResponse(existing, ds.Name))
}

func (s *Server) handleDeleteQueryTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	names, err := s.syncJobNamesUsingQueryTemplate(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "检查任务引用失败")
		return
	}
	if len(names) > 0 {
		writeError(w, http.StatusConflict, "query_template_in_use",
			fmt.Sprintf("数据集被任务 %s 引用，请先调整这些任务", strings.Join(names, "、")))
		return
	}

	if err := s.dataStore.DeleteQueryTemplate(id); err != nil {
		if errors.Is(err, store.ErrQueryTemplateNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "数据集不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "删除数据集失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) syncJobNamesUsingQueryTemplate(templateID int64) ([]string, error) {
	jobs, err := s.dataStore.ListSyncJobs("")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, job := range jobs {
		if job.AuthoringMode != "form" {
			continue
		}
		var def formJobDefinition
		if err := json.Unmarshal([]byte(job.Definition), &def); err != nil {
			continue
		}
		if def.Reader.TemplateID != nil && *def.Reader.TemplateID == templateID {
			names = append(names, job.Name)
		}
	}
	return names, nil
}

func toQueryTemplateResponse(template store.QueryTemplate, dataSourceName string) queryTemplateResponse {
	return queryTemplateResponse{
		ID:             template.ID,
		Name:           template.Name,
		Description:    template.Description,
		DataSourceID:   template.DataSourceID,
		DataSourceName: dataSourceName,
		SQL:            template.SQLText,
		Table:          template.TableName,
		Columns:        template.ColumnList(),
		Where:          template.WhereClause,
		SplitPk:        template.SplitPk,
		CreatedAt:      template.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      template.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
