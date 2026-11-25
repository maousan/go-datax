// Package connectiontest 对数据源执行轻量的连通性检查。
package connectiontest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"

	"github.com/longkeyy/go-datax/internal/dbdsn"
	"github.com/longkeyy/go-datax/internal/oracledsn"
	"github.com/longkeyy/go-datax/internal/sqlserverdsn"
)

var testTimeout = 5 * time.Second

// Params 是连接测试所需的全部信息。
type Params struct {
	Type     string
	JDBCUrl  string
	Username string
	Password string
}

// Result 描述一次连接测试的结果。
type Result struct {
	OK        bool
	LatencyMs int64
	Message   string
	Version   string
}

// Open 按数据源参数创建数据库连接。调用方负责关闭返回的 *sql.DB。
func Open(params Params) (*sql.DB, string, error) {
	driver, dsn, err := buildDSN(params)
	if err != nil {
		return nil, "", err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, "", err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, driver, nil
}

// Test 建立连接并执行 SELECT 1。
func Test(ctx context.Context, params Params) Result {
	start := time.Now()

	db, driver, err := Open(params)
	if err != nil {
		return Result{Message: err.Error()}
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return Result{Message: err.Error()}
	}

	var one int
	probe := "SELECT 1"
	if driver == "oracle" {
		probe = "SELECT 1 FROM DUAL"
	}
	if err := db.QueryRowContext(ctx, probe).Scan(&one); err != nil {
		return Result{Message: err.Error()}
	}

	return Result{
		OK:        true,
		LatencyMs: time.Since(start).Milliseconds(),
		Message:   "连接成功",
		Version:   queryVersion(ctx, db, driver),
	}
}

// queryVersion 尽力获取数据库版本；失败时返回空字符串，不影响连接结论。
func queryVersion(ctx context.Context, db *sql.DB, driver string) string {
	var query string
	switch driver {
	case "mysql":
		query = "SELECT VERSION()"
	case "pgx":
		query = "SELECT version()"
	case "sqlite":
		query = "SELECT sqlite_version()"
	case "sqlserver":
		query = "SELECT @@VERSION"
	case "oracle":
		query = "SELECT banner FROM v$version WHERE ROWNUM = 1"
	default:
		return ""
	}

	var version sql.NullString
	if err := db.QueryRowContext(ctx, query).Scan(&version); err != nil {
		return ""
	}
	return strings.TrimSpace(version.String)
}

func buildDSN(params Params) (string, string, error) {
	switch strings.ToLower(params.Type) {
	case "mysql":
		dsn, err := dbdsn.MySQL(params.JDBCUrl, params.Username, params.Password, testTimeout)
		if err != nil {
			return "", "", err
		}
		return "mysql", dsn, nil
	case "postgres":
		dsn, err := dbdsn.Postgres(params.JDBCUrl, params.Username, params.Password, testTimeout)
		if err != nil {
			return "", "", err
		}
		return "pgx", dsn, nil
	case "sqlite":
		return "sqlite", sqliteReadOnlyDSN(strings.TrimPrefix(params.JDBCUrl, "jdbc:sqlite:")), nil
	case "mssql":
		dsn, err := sqlserverdsn.Build(params.JDBCUrl, params.Username, params.Password)
		if err != nil {
			return "", "", err
		}
		return "sqlserver", dsn, nil
	case "oracle":
		dsn, err := oracledsn.Build(params.JDBCUrl, params.Username, params.Password)
		if err != nil {
			return "", "", err
		}
		return "oracle", dsn, nil
	default:
		return "", "", fmt.Errorf("暂不支持 %q 类型的连接测试", params.Type)
	}
}

// sqliteReadOnlyDSN 以只读方式打开，避免连接测试创建任意路径的文件。
func sqliteReadOnlyDSN(path string) string {
	return "file:" + strings.ReplaceAll(path, "\\", "/") + "?mode=ro"
}
