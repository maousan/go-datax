package connector

import (
	"context"
	"database/sql"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/longkeyy/go-datax/internal/dbdsn"
)

var postgresDialect = sqlDialect{
	name:        "postgres",
	driver:      "pgx",
	quote:       func(name string) string { return quoteWith(name, `"`) },
	placeholder: dollarPlaceholder,
	mapType:     mapPostgresType,
	open: func(cfg Config) (string, string, error) {
		dsn, err := dbdsn.Postgres(cfg.JDBCUrl, cfg.Username, cfg.Password, databaseConnectTimeout)
		return "pgx", dsn, err
	},
	driverValue: func(value any) any { return value },
	upsertSQL:   postgresUpsertSQL,
	primaryKey:  postgresPrimaryKey,
	truncateSQL: func(table string) string { return "TRUNCATE TABLE " + quoteWith(table, `"`) },
}

func mapPostgresType(raw string) DataType {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "INT2", "INT4", "INT8", "SMALLINT", "INTEGER", "BIGINT", "OID":
		return TypeInt64
	case "FLOAT4", "FLOAT8", "REAL", "DOUBLE PRECISION":
		return TypeFloat64
	case "NUMERIC", "DECIMAL":
		return TypeDecimal
	case "BOOL", "BOOLEAN":
		return TypeBool
	case "BYTEA":
		return TypeBytes
	case "DATE", "TIMESTAMP", "TIMESTAMPTZ":
		return TypeTimestamp
	default:
		return TypeString
	}
}

func postgresUpsertSQL(table string, schema Schema, primaryKey []string) string {
	quote := func(name string) string { return quoteWith(name, `"`) }
	quotedTable := quote(table)
	names := quotedNames(schema, quote)
	statement := "INSERT INTO " + quotedTable + " (" + strings.Join(names, ", ") + ")" +
		" VALUES (" + strings.Join(placeholders(len(schema), dollarPlaceholder), ", ") + ")"

	primary := make(map[string]bool, len(primaryKey))
	keys := make([]string, 0, len(primaryKey))
	for _, name := range primaryKey {
		primary[strings.ToLower(name)] = true
		keys = append(keys, quote(name))
	}
	updates := make([]string, 0, len(schema))
	for _, column := range schema {
		if primary[strings.ToLower(column.Name)] {
			continue
		}
		quoted := quote(column.Name)
		updates = append(updates, quoted+"=EXCLUDED."+quoted)
	}
	if len(updates) == 0 {
		return statement + " ON CONFLICT (" + strings.Join(keys, ", ") + ") DO NOTHING"
	}
	return statement + " ON CONFLICT (" + strings.Join(keys, ", ") + ") DO UPDATE SET " + strings.Join(updates, ", ")
}

func postgresPrimaryKey(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	schemaName, tableName := splitTableName(table)
	if schemaName == "" {
		if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schemaName); err != nil {
			return nil, err
		}
	}
	query := `SELECT kcu.column_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
 AND tc.table_schema = kcu.table_schema
 AND tc.table_name = kcu.table_name
WHERE tc.constraint_type = 'PRIMARY KEY'
  AND tc.table_schema = $1
  AND tc.table_name = $2
ORDER BY kcu.ordinal_position`
	return primaryKeyColumns(ctx, db, query, schemaName, tableName)
}
