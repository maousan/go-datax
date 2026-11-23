// Package dbexplorer 提供数据源的只读结构浏览与查询能力。
package dbexplorer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/connectiontest"
	"github.com/longkeyy/go-datax/internal/store"
)

// ErrInvalidQuery 表示查询语句不符合只读查询约束。
var ErrInvalidQuery = errors.New("only read-only queries are allowed")

var dangerousSQL = regexp.MustCompile(`(?i)\b(insert|update|delete|merge|drop|alter|create|truncate|grant|revoke|call|exec|execute|into|for\s+update)\b`)

// Schema 是数据库中的 schema/数据库。
type Schema struct {
	Name       string `json:"name"`
	TableCount int64  `json:"tableCount"`
}

// Table 是 schema 下的表或视图。
type Table struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Column 是表的字段信息。
type Column struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Nullable bool   `json:"nullable"`
	Default  string `json:"default"`
	Comment  string `json:"comment"`
	Position int    `json:"position"`
}

// RowsResult 是分页浏览表数据的结果。
type RowsResult struct {
	Columns  []string `json:"columns"`
	Rows     [][]any  `json:"rows"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"pageSize"`
}

// QueryResult 是 SQL 查询结果。
type QueryResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	ElapsedMs int64    `json:"elapsedMs"`
	Truncated bool     `json:"truncated"`
}

// Explorer 持有到某个数据源的只读连接。
type Explorer struct {
	db      *sql.DB
	dialect string
}

// Open 创建数据源连接并验证可用性。
func Open(ctx context.Context, ds store.DataSource) (*Explorer, error) {
	db, driver, err := connectiontest.Open(connectiontest.Params{
		Type:     ds.Type,
		JDBCUrl:  ds.JDBCUrl,
		Username: ds.Username,
		Password: ds.Password,
	})
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, err
	}

	return &Explorer{db: db, dialect: dialectOf(driver)}, nil
}

// Close 释放底层连接。
func (e *Explorer) Close() error { return e.db.Close() }

func dialectOf(driver string) string {
	switch driver {
	case "pgx":
		return "postgres"
	case "sqlserver":
		return "mssql"
	default:
		return driver
	}
}

// Schemas 返回当前账号可访问的 schema / 数据库。
func (e *Explorer) Schemas(ctx context.Context) ([]Schema, error) {
	var query string
	switch e.dialect {
	case "sqlite":
		_, rows, err := queryRows(ctx, e.db, `SELECT name FROM sqlite_master WHERE type IN ('table','view')`)
		if err != nil {
			return nil, err
		}
		return []Schema{{Name: "main", TableCount: int64(len(rows))}}, nil
	case "mysql":
		query = `SELECT s.schema_name, COUNT(t.table_name)
			FROM information_schema.schemata s
			LEFT JOIN information_schema.tables t ON t.table_schema = s.schema_name
			WHERE s.schema_name NOT IN ('information_schema','mysql','performance_schema','sys')
			GROUP BY s.schema_name ORDER BY s.schema_name`
	case "postgres":
		query = `SELECT s.schema_name, COUNT(t.table_name)
			FROM information_schema.schemata s
			LEFT JOIN information_schema.tables t ON t.table_schema = s.schema_name
			WHERE s.schema_name NOT IN ('pg_catalog','information_schema')
			  AND s.schema_name NOT LIKE 'pg_toast%'
			GROUP BY s.schema_name ORDER BY s.schema_name`
	case "mssql":
		query = `SELECT s.name, COUNT(t.TABLE_NAME)
			FROM sys.schemas s
			LEFT JOIN INFORMATION_SCHEMA.TABLES t ON t.TABLE_SCHEMA = s.name
			WHERE s.name NOT IN ('sys','INFORMATION_SCHEMA','guest')
			GROUP BY s.name ORDER BY s.name`
	case "oracle":
		query = `SELECT owner, COUNT(*) FROM all_tables GROUP BY owner ORDER BY owner`
	default:
		return nil, fmt.Errorf("暂不支持 %s 类型的数据源", e.dialect)
	}

	columns, rows, err := queryRows(ctx, e.db, query)
	if err != nil {
		return nil, err
	}
	if len(columns) < 2 {
		return nil, fmt.Errorf("读取 schema 失败")
	}

	schemas := make([]Schema, 0, len(rows))
	for _, row := range rows {
		schemas = append(schemas, Schema{
			Name:       asString(row[0]),
			TableCount: asInt64(row[1]),
		})
	}
	return schemas, nil
}

// Tables 返回 schema 下的表和视图。
func (e *Explorer) Tables(ctx context.Context, schema string) ([]Table, error) {
	var (
		query string
		args  []any
	)

	switch e.dialect {
	case "sqlite":
		query = `SELECT name, CASE type WHEN 'view' THEN 'view' ELSE 'table' END
			FROM sqlite_master
			WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%'
			ORDER BY name`
	case "mysql", "postgres", "mssql":
		query = fmt.Sprintf(`SELECT table_name, table_type FROM information_schema.tables
			WHERE table_schema = %s AND table_type IN ('BASE TABLE','VIEW')
			ORDER BY table_name`, e.placeholder(1))
		args = []any{schema}
	case "oracle":
		query = fmt.Sprintf(`SELECT table_name, 'table' FROM all_tables WHERE owner = %s
			UNION ALL
			SELECT view_name, 'view' FROM all_views WHERE owner = %s
			ORDER BY 1`, e.placeholder(1), e.placeholder(2))
		args = []any{schema, schema}
	default:
		return nil, fmt.Errorf("暂不支持 %s 类型的数据源", e.dialect)
	}

	columns, rows, err := queryRows(ctx, e.db, query, args...)
	if err != nil {
		return nil, err
	}
	if len(columns) < 2 {
		return nil, fmt.Errorf("读取表列表失败")
	}

	tables := make([]Table, 0, len(rows))
	for _, row := range rows {
		tableType := "table"
		if strings.EqualFold(asString(row[1]), "view") {
			tableType = "view"
		}
		tables = append(tables, Table{Name: asString(row[0]), Type: tableType})
	}
	return tables, nil
}

// Columns 返回表的字段信息。
func (e *Explorer) Columns(ctx context.Context, schema, table string) ([]Column, error) {
	if strings.TrimSpace(table) == "" {
		return nil, fmt.Errorf("表名不能为空")
	}
	if e.dialect == "sqlite" {
		_, rows, err := queryRows(ctx, e.db, fmt.Sprintf("PRAGMA table_info(%s)", quoteIdent(e.dialect, table)))
		if err != nil {
			return nil, err
		}
		columns := make([]Column, 0, len(rows))
		for _, row := range rows {
			if len(row) < 6 {
				continue
			}
			columns = append(columns, Column{
				Name:     asString(row[1]),
				DataType: asString(row[2]),
				Nullable: asInt64(row[3]) == 0,
				Default:  asString(row[4]),
				Position: int(asInt64(row[0])) + 1,
			})
		}
		return columns, nil
	}

	var query string
	args := []any{schema, table}
	switch e.dialect {
	case "mysql":
		query = fmt.Sprintf(`SELECT column_name, column_type, is_nullable,
			COALESCE(column_default,''), COALESCE(column_comment,''), ordinal_position
			FROM information_schema.columns
			WHERE table_schema = %s AND table_name = %s
			ORDER BY ordinal_position`, e.placeholder(1), e.placeholder(2))
	case "postgres":
		query = fmt.Sprintf(`SELECT column_name, data_type, is_nullable,
			COALESCE(column_default,''), '', ordinal_position
			FROM information_schema.columns
			WHERE table_schema = %s AND table_name = %s
			ORDER BY ordinal_position`, e.placeholder(1), e.placeholder(2))
	case "mssql":
		query = fmt.Sprintf(`SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE,
			COALESCE(COLUMN_DEFAULT,''), '', ORDINAL_POSITION
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = %s AND TABLE_NAME = %s
			ORDER BY ORDINAL_POSITION`, e.placeholder(1), e.placeholder(2))
	case "oracle":
		query = fmt.Sprintf(`SELECT c.column_name, c.data_type, c.nullable,
			COALESCE(c.data_default,''), COALESCE(cc.comments,''), c.column_id
			FROM all_tab_columns c
			LEFT JOIN all_col_comments cc
			  ON cc.owner = c.owner AND cc.table_name = c.table_name AND cc.column_name = c.column_name
			WHERE c.owner = %s AND c.table_name = %s
			ORDER BY c.column_id`, e.placeholder(1), e.placeholder(2))
	default:
		return nil, fmt.Errorf("暂不支持 %s 类型的数据源", e.dialect)
	}

	_, rows, err := queryRows(ctx, e.db, query, args...)
	if err != nil {
		return nil, err
	}

	columns := make([]Column, 0, len(rows))
	for _, row := range rows {
		if len(row) < 6 {
			continue
		}
		columns = append(columns, Column{
			Name:     asString(row[0]),
			DataType: asString(row[1]),
			Nullable: strings.EqualFold(asString(row[2]), "YES") || strings.EqualFold(asString(row[2]), "Y"),
			Default:  asString(row[3]),
			Comment:  asString(row[4]),
			Position: int(asInt64(row[5])),
		})
	}
	return columns, nil
}

// Rows 分页读取表数据，where/orderBy 为用户输入的 SQL 片段。
func (e *Explorer) Rows(
	ctx context.Context,
	schema, table, where, orderBy string,
	page, pageSize int,
) (RowsResult, error) {
	if strings.TrimSpace(table) == "" {
		return RowsResult{}, fmt.Errorf("表名不能为空")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	where, err := safeFragment(where)
	if err != nil {
		return RowsResult{}, err
	}
	orderBy, err = safeFragment(orderBy)
	if err != nil {
		return RowsResult{}, err
	}

	qualified := e.qualifiedTable(schema, table)
	countQuery := "SELECT COUNT(*) FROM " + qualified
	if where != "" {
		countQuery += " WHERE " + where
	}

	var total int64
	if err := e.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return RowsResult{}, err
	}

	offset := (page - 1) * pageSize
	selectBase := "SELECT * FROM " + qualified
	if where != "" {
		selectBase += " WHERE " + where
	}

	trimPaginationColumn := false
	var selectQuery string
	switch e.dialect {
	case "mssql":
		if offset == 0 {
			// SQL Server 2008 不支持 OFFSET/FETCH，使用 TOP。
			selectQuery = strings.Replace(
				selectBase, "SELECT *", fmt.Sprintf("SELECT TOP (%d) *", pageSize), 1,
			)
			if orderBy != "" {
				selectQuery += " ORDER BY " + orderBy
			}
		} else {
			rowOrder := orderBy
			if rowOrder == "" {
				rowOrder = "(SELECT NULL)"
			}
			selectQuery = fmt.Sprintf(
				"SELECT * FROM (SELECT inner_q.*, ROW_NUMBER() OVER (ORDER BY %s) AS dx_rn FROM (%s) inner_q) ranked WHERE dx_rn > %d AND dx_rn <= %d",
				rowOrder, selectBase, offset, offset+pageSize,
			)
			trimPaginationColumn = true
		}
	case "oracle":
		// Oracle 11g 不支持 OFFSET/FETCH，使用 ROWNUM 包裹以兼容旧版本。
		base := selectBase
		if orderBy != "" {
			base += " ORDER BY " + orderBy
		}
		if offset == 0 {
			selectQuery = fmt.Sprintf("SELECT * FROM (%s) WHERE ROWNUM <= %d", base, pageSize)
		} else {
			selectQuery = fmt.Sprintf(
				"SELECT * FROM (SELECT inner_q.*, ROWNUM AS dx_rn FROM (%s) inner_q WHERE ROWNUM <= %d) WHERE dx_rn > %d",
				base, offset+pageSize, offset,
			)
			trimPaginationColumn = true
		}
	default:
		selectQuery = selectBase
		if orderBy != "" {
			selectQuery += " ORDER BY " + orderBy
		}
		selectQuery += fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, offset)
	}

	columns, rows, err := queryRows(ctx, e.db, selectQuery)
	if err != nil {
		return RowsResult{}, err
	}
	if trimPaginationColumn && len(columns) > 0 {
		columns = columns[:len(columns)-1]
		for i := range rows {
			if len(rows[i]) > 0 {
				rows[i] = rows[i][:len(rows[i])-1]
			}
		}
	}
	return RowsResult{
		Columns:  columns,
		Rows:     rows,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// Query 执行只读 SQL，最多返回 limit 行。
func (e *Explorer) Query(ctx context.Context, statement string, limit int) (QueryResult, error) {
	statement = strings.TrimSpace(statement)
	if err := validateReadOnlySQL(statement); err != nil {
		return QueryResult{}, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	start := time.Now()
	rows, err := e.db.QueryContext(ctx, statement)
	if err != nil {
		return QueryResult{}, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return QueryResult{}, err
	}

	result := QueryResult{Columns: columns, Rows: make([][]any, 0, limit)}
	for rows.Next() {
		if len(result.Rows) >= limit {
			result.Truncated = true
			break
		}
		row, err := scanCurrentRow(rows, len(columns))
		if err != nil {
			return QueryResult{}, err
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, err
	}
	result.ElapsedMs = time.Since(start).Milliseconds()
	return result, nil
}

func validateReadOnlySQL(statement string) error {
	if statement == "" {
		return fmt.Errorf("%w: SQL 不能为空", ErrInvalidQuery)
	}

	trimmed := strings.TrimRight(statement, "; \t\r\n")
	if trimmed == "" {
		return fmt.Errorf("%w: SQL 不能为空", ErrInvalidQuery)
	}
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("%w: 不支持多条语句", ErrInvalidQuery)
	}
	if strings.Contains(trimmed, "--") || strings.Contains(trimmed, "/*") || strings.Contains(trimmed, "*/") {
		return fmt.Errorf("%w: 不支持 SQL 注释", ErrInvalidQuery)
	}
	if dangerousSQL.MatchString(trimmed) {
		return fmt.Errorf("%w: 仅允许查询语句", ErrInvalidQuery)
	}

	first := strings.ToUpper(strings.Fields(trimmed)[0])
	switch first {
	case "SELECT", "WITH", "SHOW", "DESCRIBE", "DESC", "EXPLAIN":
		return nil
	default:
		return fmt.Errorf("%w: 仅允许 SELECT/WITH/SHOW/DESCRIBE/EXPLAIN", ErrInvalidQuery)
	}
}

func (e *Explorer) qualifiedTable(schema, table string) string {
	if e.dialect == "sqlite" || strings.TrimSpace(schema) == "" {
		return quoteIdent(e.dialect, table)
	}
	return quoteIdent(e.dialect, schema) + "." + quoteIdent(e.dialect, table)
}

func (e *Explorer) placeholder(n int) string {
	switch e.dialect {
	case "postgres":
		return fmt.Sprintf("$%d", n)
	case "mssql":
		return fmt.Sprintf("@p%d", n)
	case "oracle":
		return fmt.Sprintf(":%d", n)
	default:
		return "?"
	}
}

func quoteIdent(dialect, name string) string {
	switch dialect {
	case "mysql":
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case "mssql":
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
}

func safeFragment(fragment string) (string, error) {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return "", nil
	}
	if strings.Contains(fragment, ";") ||
		strings.Contains(fragment, "--") ||
		strings.Contains(fragment, "/*") ||
		strings.Contains(fragment, "*/") {
		return "", fmt.Errorf("%w: 查询条件不合法", ErrInvalidQuery)
	}
	return fragment, nil
}

func queryRows(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	result := make([][]any, 0)
	for rows.Next() {
		row, err := scanCurrentRow(rows, len(columns))
		if err != nil {
			return nil, nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return columns, result, nil
}

func scanCurrentRow(rows *sql.Rows, count int) ([]any, error) {
	raw := make([]any, count)
	pointers := make([]any, count)
	for i := range raw {
		pointers[i] = &raw[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, err
	}
	for i, value := range raw {
		raw[i] = normalizeValue(value)
	}
	return raw, nil
}

func normalizeValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return v
	}
}

func asString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case []byte:
		return string(v)
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func asInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case uint64:
		return int64(v)
	case []byte:
		var result int64
		_, _ = fmt.Sscan(string(v), &result)
		return result
	case string:
		var result int64
		_, _ = fmt.Sscan(v, &result)
		return result
	default:
		return 0
	}
}
