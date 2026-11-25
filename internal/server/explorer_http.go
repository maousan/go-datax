package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/dbexplorer"
	"github.com/longkeyy/go-datax/internal/store"
)

const (
	explorerMetadataTimeout = 30 * time.Second
	explorerQueryTimeout    = 20 * time.Second
)

func (s *Server) handleDataSourceSchemas(w http.ResponseWriter, r *http.Request) {
	s.withExplorer(w, r, explorerMetadataTimeout, func(ctx context.Context, explorer *dbexplorer.Explorer) {
		schemas, err := explorer.Schemas(ctx)
		if err != nil {
			writeError(w, http.StatusBadGateway, "explorer_error", "读取数据库结构失败："+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"schemas": schemas})
	})
}

func (s *Server) handleDataSourceTables(w http.ResponseWriter, r *http.Request) {
	schema := r.URL.Query().Get("schema")
	s.withExplorer(w, r, explorerMetadataTimeout, func(ctx context.Context, explorer *dbexplorer.Explorer) {
		tables, err := explorer.Tables(ctx, schema)
		if err != nil {
			writeError(w, http.StatusBadGateway, "explorer_error", "读取表列表失败："+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tables": tables})
	})
}

func (s *Server) handleDataSourceColumns(w http.ResponseWriter, r *http.Request) {
	schema := r.URL.Query().Get("schema")
	table := r.URL.Query().Get("table")
	if strings.TrimSpace(table) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "缺少 table 参数")
		return
	}

	s.withExplorer(w, r, explorerMetadataTimeout, func(ctx context.Context, explorer *dbexplorer.Explorer) {
		columns, err := explorer.Columns(ctx, schema, table)
		if err != nil {
			writeError(w, http.StatusBadGateway, "explorer_error", "读取字段信息失败："+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"columns": columns})
	})
}

func (s *Server) handleDataSourceRows(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	schema := query.Get("schema")
	table := query.Get("table")
	if strings.TrimSpace(table) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "缺少 table 参数")
		return
	}

	page, _ := strconv.Atoi(query.Get("page"))
	pageSize, _ := strconv.Atoi(query.Get("pageSize"))

	s.withExplorer(w, r, explorerMetadataTimeout, func(ctx context.Context, explorer *dbexplorer.Explorer) {
		result, err := explorer.Rows(ctx, schema, table, query.Get("where"), query.Get("orderBy"), page, pageSize)
		if err != nil {
			if errors.Is(err, dbexplorer.ErrInvalidQuery) {
				writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
				return
			}
			writeError(w, http.StatusBadGateway, "explorer_error", "读取表数据失败："+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

type explorerQueryRequest struct {
	SQL   string `json:"sql"`
	Limit int    `json:"limit"`
}

func (s *Server) handleDataSourceQuery(w http.ResponseWriter, r *http.Request) {
	var req explorerQueryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}

	s.withExplorer(w, r, explorerQueryTimeout, func(ctx context.Context, explorer *dbexplorer.Explorer) {
		result, err := explorer.Query(ctx, req.SQL, req.Limit)
		if err != nil {
			if errors.Is(err, dbexplorer.ErrInvalidQuery) {
				writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
				return
			}
			writeError(w, http.StatusBadGateway, "query_error", "查询执行失败："+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func (s *Server) withExplorer(
	w http.ResponseWriter,
	r *http.Request,
	timeout time.Duration,
	fn func(context.Context, *dbexplorer.Explorer),
) {
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
	if ds.Type == "filesystem" {
		writeError(w, http.StatusBadRequest, "unsupported_operation", "文件系统数据源不支持数据库浏览")
		return
	}

	explorer, err := dbexplorer.Open(r.Context(), ds)
	if err != nil {
		writeError(w, http.StatusBadGateway, "connect_failed", "连接数据源失败："+err.Error())
		return
	}
	defer explorer.Close()

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	fn(ctx, explorer)
}
