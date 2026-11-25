package connector

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

const defaultBatchSize = 256

func validateSQLite(cfg Config) error {
	if err := validateOptionKeys(cfg.Options, "table", "query", "splitPk", "writeMode"); err != nil {
		return err
	}
	table := strings.TrimSpace(cfg.Options["table"])
	query := strings.TrimSpace(cfg.Options["query"])
	if table != "" && query != "" {
		return fmt.Errorf("sqlite connector accepts either table or query, not both")
	}
	if table == "" && query == "" {
		return fmt.Errorf("sqlite connector requires table or query")
	}
	if splitPk := strings.TrimSpace(cfg.Options["splitPk"]); splitPk != "" && table == "" {
		return fmt.Errorf("splitPk requires table mode")
	}
	if mode := strings.TrimSpace(cfg.Options["writeMode"]); mode != "" && mode != "insert" && mode != "truncate" {
		return fmt.Errorf("writeMode must be insert or truncate")
	}
	return nil
}

func validateSQLiteSink(cfg Config) error {
	if strings.TrimSpace(cfg.Options["query"]) != "" {
		return fmt.Errorf("sqlite sink does not support query")
	}
	if strings.TrimSpace(cfg.Options["table"]) == "" {
		return fmt.Errorf("sqlite sink requires table")
	}
	if strings.TrimSpace(cfg.Options["splitPk"]) != "" {
		return fmt.Errorf("splitPk is only supported by sqlite sources")
	}
	return nil
}

func sqlitePath(cfg Config) (string, error) {
	path := strings.TrimSpace(strings.TrimPrefix(cfg.JDBCUrl, "jdbc:sqlite:"))
	if path == "" {
		path = strings.TrimSpace(cfg.DataSourceProperties["path"])
	}
	if path == "" {
		return "", fmt.Errorf("sqlite data source has no file path")
	}
	return path, nil
}

func quoteIdentifier(name string) string {
	parts := strings.Split(name, ".")
	for index, part := range parts {
		parts[index] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

func discoverSQLite(ctx context.Context, cfg Config) (Schema, error) {
	path, err := sqlitePath(cfg)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	table := strings.TrimSpace(cfg.Options["table"])
	query := strings.TrimSpace(cfg.Options["query"])
	if query != "" {
		normalized := strings.TrimSpace(strings.TrimSuffix(query, ";"))
		if normalized == "" {
			return nil, fmt.Errorf("query is empty")
		}
		probe := "SELECT * FROM (" + normalized + ") LIMIT 0"
		rows, err := db.QueryContext(ctx, probe)
		if err != nil {
			return nil, fmt.Errorf("inspect query: %w", err)
		}
		defer rows.Close()
		types, err := rows.ColumnTypes()
		if err != nil {
			return nil, err
		}
		schema := make(Schema, 0, len(types))
		for _, columnType := range types {
			schema = append(schema, Column{
				Name: columnType.Name(),
				Type: mapSQLiteType(columnType.DatabaseTypeName()),
			})
		}
		return schema, schema.Validate()
	}

	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	schema := Schema{}
	for rows.Next() {
		var (
			cid      int
			name     string
			rawType  string
			notNull  int
			defaultV sql.NullString
			primary  int
		)
		if err := rows.Scan(&cid, &name, &rawType, &notNull, &defaultV, &primary); err != nil {
			return nil, err
		}
		schema = append(schema, Column{Name: name, Type: mapSQLiteType(rawType)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return schema, schema.Validate()
}

func mapSQLiteType(raw string) DataType {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "int"):
		return TypeInt64
	case strings.Contains(lower, "real"), strings.Contains(lower, "floa"), strings.Contains(lower, "doub"):
		return TypeFloat64
	case strings.Contains(lower, "dec"), strings.Contains(lower, "num"), strings.Contains(lower, "money"):
		return TypeDecimal
	case strings.Contains(lower, "blob"):
		return TypeBytes
	case strings.Contains(lower, "bool"):
		return TypeBool
	case strings.Contains(lower, "date"), strings.Contains(lower, "time"):
		return TypeTimestamp
	default:
		return TypeString
	}
}

func scanCanonicalRow(rows *sql.Rows, schema Schema) ([]any, error) {
	raw := make([]any, len(schema))
	pointers := make([]any, len(schema))
	for index := range raw {
		pointers[index] = &raw[index]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, err
	}
	row := make([]any, len(schema))
	for index, value := range raw {
		converted, err := coerceValue(value, schema[index].Type)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", schema[index].Name, err)
		}
		row[index] = converted
	}
	return row, nil
}

var sqliteDialect = sqlDialect{
	name:        "sqlite",
	driver:      "sqlite",
	quote:       quoteIdentifier,
	placeholder: questionPlaceholder,
	mapType:     mapSQLiteType,
	open: func(cfg Config) (string, string, error) {
		path, err := sqlitePath(cfg)
		return "sqlite", path, err
	},
	driverValue: sqliteDriverValue,
	truncateSQL: func(table string) string { return "DELETE FROM " + quoteIdentifier(table) },
}

func sqliteDriverValue(value any) any {
	switch typed := value.(type) {
	case bool:
		if typed {
			return int64(1)
		}
		return int64(0)
	case time.Time:
		return typed.Format(time.RFC3339Nano)
	default:
		return value
	}
}
