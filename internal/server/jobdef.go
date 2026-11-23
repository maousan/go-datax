package server

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/longkeyy/go-datax/internal/store"
)

var pluginsByDataSourceType = map[string][2]string{
	"mysql":    {"mysqlreader", "mysqlwriter"},
	"postgres": {"postgresqlreader", "postgresqlwriter"},
	"sqlite":   {"sqlitereader", "sqlitewriter"},
	"mssql":    {"sqlserverreader", "sqlserverwriter"},
	"oracle":   {"oraclereader", "oraclewriter"},
}

type formEndpoint struct {
	DataSourceID int64    `json:"dataSourceId"`
	Table        string   `json:"table"`
	Column       []string `json:"column"`
	Where        string   `json:"where,omitempty"`
	SplitPk      string   `json:"splitPk,omitempty"`
	WriteMode    string   `json:"writeMode,omitempty"`
}

type jobErrorLimit struct {
	Record     int     `json:"record"`
	Percentage float64 `json:"percentage"`
}

type jobSetting struct {
	Channel    int           `json:"channel"`
	ErrorLimit jobErrorLimit `json:"errorLimit"`
}

type formJobDefinition struct {
	Reader  formEndpoint `json:"reader"`
	Writer  formEndpoint `json:"writer"`
	Setting jobSetting   `json:"setting"`
}

type syncJobRequest struct {
	Name                 string             `json:"name"`
	AuthoringMode        string             `json:"authoringMode"`
	Form                 *formJobDefinition `json:"form,omitempty"`
	Raw                  json.RawMessage    `json:"raw,omitempty"`
	RetryMax             int                `json:"retryMax"`
	RetryIntervalSeconds int                `json:"retryIntervalSeconds"`
}

type syncJobResponse struct {
	ID                   int64              `json:"id"`
	Name                 string             `json:"name"`
	AuthoringMode        string             `json:"authoringMode"`
	Form                 *formJobDefinition `json:"form,omitempty"`
	Raw                  json.RawMessage    `json:"raw,omitempty"`
	Summary              string             `json:"summary"`
	RetryMax             int                `json:"retryMax"`
	RetryIntervalSeconds int                `json:"retryIntervalSeconds"`
	LastRun              *runResponse       `json:"lastRun,omitempty"`
	CreatedAt            string             `json:"createdAt"`
	UpdatedAt            string             `json:"updatedAt"`
}

func (req syncJobRequest) normalized() (syncJobRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.AuthoringMode = strings.ToLower(strings.TrimSpace(req.AuthoringMode))

	if req.Name == "" {
		return req, fmt.Errorf("名称不能为空")
	}
	if len([]rune(req.Name)) > 100 {
		return req, fmt.Errorf("名称不能超过 100 个字符")
	}

	if req.RetryMax < 0 || req.RetryMax > 10 {
		return req, fmt.Errorf("失败重试次数必须在 0 到 10 之间")
	}
	if req.RetryIntervalSeconds < 0 || req.RetryIntervalSeconds > 86400 {
		return req, fmt.Errorf("重试间隔必须在 0 到 86400 秒之间")
	}
	if req.RetryMax == 0 {
		req.RetryIntervalSeconds = 0
	}

	switch req.AuthoringMode {
	case "form":
		if req.Form == nil {
			return req, fmt.Errorf("表单模式缺少配置")
		}
		def := *req.Form
		def.Reader.Table = strings.TrimSpace(def.Reader.Table)
		def.Writer.Table = strings.TrimSpace(def.Writer.Table)
		def.Reader.Where = strings.TrimSpace(def.Reader.Where)
		def.Reader.SplitPk = strings.TrimSpace(def.Reader.SplitPk)
		def.Writer.WriteMode = strings.TrimSpace(def.Writer.WriteMode)
		if def.Writer.WriteMode == "" {
			def.Writer.WriteMode = "insert"
		}
		if def.Setting.Channel <= 0 {
			def.Setting.Channel = 1
		}
		if def.Setting.Channel > 32 {
			return req, fmt.Errorf("并发通道数必须在 1 到 32 之间")
		}
		if def.Reader.DataSourceID <= 0 || def.Writer.DataSourceID <= 0 {
			return req, fmt.Errorf("必须选择读取与写入数据源")
		}
		if def.Reader.Table == "" || def.Writer.Table == "" {
			return req, fmt.Errorf("表名不能为空")
		}
		if len(def.Reader.Column) == 0 || len(def.Writer.Column) == 0 {
			return req, fmt.Errorf("列不能为空")
		}
		if len(def.Reader.Column) != len(def.Writer.Column) {
			return req, fmt.Errorf("读取列与写入列数量必须一致")
		}
		req.Form = &def

	case "raw":
		if len(req.Raw) == 0 {
			return req, fmt.Errorf("原始模式缺少 DataX 配置")
		}
		if err := validateRawConfig(req.Raw); err != nil {
			return req, err
		}

	default:
		return req, fmt.Errorf("编写模式必须是 form 或 raw")
	}

	return req, nil
}

func validateRawConfig(raw json.RawMessage) error {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("JSON 格式不正确: %v", err)
	}

	job, ok := config["job"].(map[string]any)
	if !ok {
		return fmt.Errorf("缺少 job 配置")
	}
	contents, ok := job["content"].([]any)
	if !ok || len(contents) == 0 {
		return fmt.Errorf("job.content 不能为空")
	}
	if len(contents) > 1 {
		return fmt.Errorf("当前执行引擎只执行 job.content[0]，多表配置不会生效；请拆分为多个任务")
	}

	for index, item := range contents {
		content, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("job.content[%d] 格式不正确", index)
		}
		if transformer, exists := content["transformer"]; exists {
			if list, ok := transformer.([]any); !ok || len(list) > 0 {
				return fmt.Errorf("transformer 功能尚未实现，job.content[%d].transformer 不会生效", index)
			}
		}
		for _, role := range []string{"reader", "writer"} {
			endpoint, ok := content[role].(map[string]any)
			if !ok {
				return fmt.Errorf("job.content[%d].%s 缺失", index, role)
			}
			if name, _ := endpoint["name"].(string); strings.TrimSpace(name) == "" {
				return fmt.Errorf("job.content[%d].%s.name 缺失", index, role)
			}
			if _, ok := endpoint["parameter"].(map[string]any); !ok {
				return fmt.Errorf("job.content[%d].%s.parameter 缺失", index, role)
			}
		}
	}
	return nil
}

var oracleUpdatePattern = regexp.MustCompile(`(?i)^update\s*\((.*)\)$`)

// validateFormWriteMode 校验表单写入模式是否被对应 writer 支持。
// strict 为 true 时用于保存，false 时用于运行旧任务（兼容历史配置）。
func validateFormWriteMode(def formJobDefinition, dataSources map[int64]store.DataSource, strict bool) error {
	writer, ok := dataSources[def.Writer.DataSourceID]
	if !ok {
		return fmt.Errorf("写入数据源不存在")
	}

	mode := strings.TrimSpace(def.Writer.WriteMode)
	if mode == "" {
		mode = "insert"
	}
	lower := strings.ToLower(mode)

	switch writer.Type {
	case "postgres", "mssql":
		if strict && lower != "insert" {
			return fmt.Errorf("%s 写入器不支持 writeMode=%s，会按主键自动 upsert", writer.Type, mode)
		}
	case "mysql", "sqlite":
		switch lower {
		case "insert", "replace", "update":
		default:
			return fmt.Errorf("%s 写入模式仅支持 insert、replace 或 update", writer.Type)
		}
	case "oracle":
		upper := strings.ToUpper(mode)
		switch {
		case upper == "INSERT" || upper == "REPLACE":
			return nil
		case upper == "UPDATE":
			return fmt.Errorf("Oracle 的 update 模式需要指定主键列，例如 update(id, code)")
		case strings.HasPrefix(upper, "UPDATE("):
			match := oracleUpdatePattern.FindStringSubmatch(mode)
			if match == nil {
				return fmt.Errorf("Oracle 的 update 模式格式应为 update(col1, col2)")
			}
			keys := splitColumnList(match[1])
			if len(keys) == 0 {
				return fmt.Errorf("Oracle 的 update 模式至少需要一个主键列")
			}
			for _, key := range keys {
				if !containsColumnFold(def.Writer.Column, key) {
					return fmt.Errorf("更新主键列 %s 不在写入列中", key)
				}
			}
		default:
			return fmt.Errorf("Oracle 写入模式仅支持 INSERT、REPLACE 或 update(col1, col2)")
		}
	}
	return nil
}

func splitColumnList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func containsColumnFold(columns []string, want string) bool {
	for _, column := range columns {
		if strings.EqualFold(strings.TrimSpace(column), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

// materialize 把表单定义展开为可执行的 DataX 配置。
func materializeForm(def formJobDefinition, dataSources map[int64]store.DataSource) (map[string]any, error) {
	readerSource, ok := dataSources[def.Reader.DataSourceID]
	if !ok {
		return nil, fmt.Errorf("读取数据源不存在")
	}
	writerSource, ok := dataSources[def.Writer.DataSourceID]
	if !ok {
		return nil, fmt.Errorf("写入数据源不存在")
	}

	readerPlugins, ok := pluginsByDataSourceType[readerSource.Type]
	if !ok {
		return nil, fmt.Errorf("不支持的数据源类型: %s", readerSource.Type)
	}
	writerPlugins, ok := pluginsByDataSourceType[writerSource.Type]
	if !ok {
		return nil, fmt.Errorf("不支持的数据源类型: %s", writerSource.Type)
	}

	readerParams := map[string]any{
		"username": readerSource.Username,
		"password": readerSource.Password,
		"connection": []map[string]any{
			{
				"jdbcUrl": readerJDBCURL(readerSource.Type, readerSource.JDBCUrl),
				"table":   []string{def.Reader.Table},
			},
		},
		"column": def.Reader.Column,
	}
	if def.Reader.Where != "" {
		readerParams["where"] = def.Reader.Where
	}
	if def.Reader.SplitPk != "" {
		readerParams["splitPk"] = def.Reader.SplitPk
	}

	writerParams := map[string]any{
		"username": writerSource.Username,
		"password": writerSource.Password,
		"connection": []map[string]any{
			{
				"jdbcUrl": writerJDBCURL(writerSource.Type, writerSource.JDBCUrl),
				"table":   []string{def.Writer.Table},
			},
		},
		"column": def.Writer.Column,
	}
	if def.Writer.WriteMode != "" {
		writerParams["writeMode"] = def.Writer.WriteMode
	}

	return map[string]any{
		"job": map[string]any{
			"setting": map[string]any{
				"speed": map[string]any{
					"channel": def.Setting.Channel,
				},
				"errorLimit": map[string]any{
					"record":     def.Setting.ErrorLimit.Record,
					"percentage": def.Setting.ErrorLimit.Percentage,
				},
			},
			"content": []map[string]any{
				{
					"reader": map[string]any{
						"name":      readerPlugins[0],
						"parameter": readerParams,
					},
					"writer": map[string]any{
						"name":      writerPlugins[1],
						"parameter": writerParams,
					},
				},
			},
		},
	}, nil
}

func readerJDBCURL(dataSourceType, jdbcURL string) any {
	if dataSourceType == "oracle" {
		return jdbcURL
	}
	return []string{jdbcURL}
}

func writerJDBCURL(dataSourceType, jdbcURL string) any {
	switch dataSourceType {
	case "postgres", "mssql", "oracle":
		return jdbcURL
	default:
		return []string{jdbcURL}
	}
}

func toSyncJobResponse(job store.SyncJob) (syncJobResponse, error) {
	response := syncJobResponse{
		ID:                   job.ID,
		Name:                 job.Name,
		AuthoringMode:        job.AuthoringMode,
		RetryMax:             job.RetryMax,
		RetryIntervalSeconds: job.RetryIntervalSeconds,
		CreatedAt:            job.CreatedAt.UTC().Format(timeLayout),
		UpdatedAt:            job.UpdatedAt.UTC().Format(timeLayout),
	}

	switch job.AuthoringMode {
	case "form":
		var def formJobDefinition
		if err := json.Unmarshal([]byte(job.Definition), &def); err != nil {
			return response, fmt.Errorf("解析任务配置失败: %v", err)
		}
		response.Form = &def
		response.Summary = def.Reader.Table + " → " + def.Writer.Table
	case "raw":
		raw := json.RawMessage(job.Definition)
		response.Raw = raw
		response.Summary = rawSummary(raw)
	}

	return response, nil
}

func rawSummary(raw json.RawMessage) string {
	var config struct {
		Job struct {
			Content []struct {
				Reader struct {
					Name string `json:"name"`
				} `json:"reader"`
				Writer struct {
					Name string `json:"name"`
				} `json:"writer"`
			} `json:"content"`
		} `json:"job"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || len(config.Job.Content) == 0 {
		return "raw"
	}
	first := config.Job.Content[0]
	return first.Reader.Name + " → " + first.Writer.Name
}

const timeLayout = "2006-01-02T15:04:05Z07:00"
