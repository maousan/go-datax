package connector

import (
	"context"
	"database/sql"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/longkeyy/go-datax/internal/dbdsn"
)

const databaseConnectTimeout = 10 * time.Second

var mysqlDialect = sqlDialect{
	name:        "mysql",
	driver:      "mysql",
	quote:       func(name string) string { return quoteWith(name, "`") },
	placeholder: questionPlaceholder,
	mapType:     mapMySQLType,
	open: func(cfg Config) (string, string, error) {
		dsn, err := dbdsn.MySQL(cfg.JDBCUrl, cfg.Username, cfg.Password, databaseConnectTimeout)
		return "mysql", dsn, err
	},
	driverValue: mysqlDriverValue,
	upsertSQL:   mysqlUpsertSQL,
	primaryKey:  mysqlPrimaryKey,
	truncateSQL: func(table string) string { return "TRUNCATE TABLE " + quoteWith(table, "`") },
}

func mapMySQLType(raw string) DataType {
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	normalized = strings.TrimPrefix(normalized, "UNSIGNED ")
	normalized = strings.TrimSuffix(normalized, " UNSIGNED")
	switch normalized {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT", "YEAR":
		return TypeInt64
	case "DECIMAL", "NUMERIC":
		return TypeDecimal
	case "FLOAT", "DOUBLE", "REAL":
		return TypeFloat64
	case "BOOL", "BOOLEAN":
		return TypeBool
	case "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY", "BIT":
		return TypeBytes
	case "DATE", "DATETIME", "TIMESTAMP":
		return TypeTimestamp
	default:
		return TypeString
	}
}

func mysqlDriverValue(value any) any {
	if typed, ok := value.(bool); ok {
		if typed {
			return int64(1)
		}
		return int64(0)
	}
	return value
}

func mysqlUpsertSQL(table string, schema Schema, primaryKey []string) string {
	quote := func(name string) string { return quoteWith(name, "`") }
	quotedTable := quote(table)
	names := quotedNames(schema, quote)
	statement := "INSERT INTO " + quotedTable + " (" + strings.Join(names, ", ") + ")" +
		" VALUES (" + strings.Join(placeholders(len(schema), questionPlaceholder), ", ") + ")"

	primary := make(map[string]bool, len(primaryKey))
	for _, name := range primaryKey {
		primary[strings.ToLower(name)] = true
	}
	updates := make([]string, 0, len(schema))
	for _, column := range schema {
		if primary[strings.ToLower(column.Name)] {
			continue
		}
		quoted := quote(column.Name)
		updates = append(updates, quoted+"=VALUES("+quoted+")")
	}
	if len(updates) == 0 {
		// Every inserted column is part of the key; keep the existing row.
		primaryColumn := quote(primaryKey[0])
		updates = append(updates, primaryColumn+"="+primaryColumn)
	}
	return statement + " ON DUPLICATE KEY UPDATE " + strings.Join(updates, ", ")
}

func mysqlPrimaryKey(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	schemaName, tableName := splitTableName(table)
	if schemaName == "" {
		if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schemaName); err != nil {
			return nil, err
		}
	}
	query := `SELECT column_name
FROM information_schema.key_column_usage
WHERE table_schema = ? AND table_name = ? AND constraint_name = 'PRIMARY'
ORDER BY ordinal_position`
	return primaryKeyColumns(ctx, db, query, schemaName, tableName)
}
