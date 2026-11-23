// Package connectiontest 对数据源执行轻量的连通性检查。
package connectiontest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"

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
}

// Test 建立连接并执行 SELECT 1。
func Test(ctx context.Context, params Params) Result {
	start := time.Now()

	driver, dsn, err := buildDSN(params)
	if err != nil {
		return Result{Message: err.Error()}
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return Result{Message: err.Error()}
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return Result{Message: err.Error()}
	}

	var one int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return Result{Message: err.Error()}
	}

	return Result{
		OK:        true,
		LatencyMs: time.Since(start).Milliseconds(),
		Message:   "连接成功",
	}
}

func buildDSN(params Params) (string, string, error) {
	switch strings.ToLower(params.Type) {
	case "mysql":
		return buildMySQLDSN(params)
	case "postgres":
		return buildPostgresDSN(params)
	case "sqlite":
		return "sqlite", sqliteReadOnlyDSN(strings.TrimPrefix(params.JDBCUrl, "jdbc:sqlite:")), nil
	case "mssql":
		dsn, err := sqlserverdsn.Build(params.JDBCUrl, params.Username, params.Password)
		if err != nil {
			return "", "", err
		}
		return "sqlserver", dsn, nil
	default:
		return "", "", fmt.Errorf("暂不支持 %q 类型的连接测试", params.Type)
	}
}

// sqliteReadOnlyDSN 以只读方式打开，避免连接测试创建任意路径的文件。
func sqliteReadOnlyDSN(path string) string {
	return "file:" + strings.ReplaceAll(path, "\\", "/") + "?mode=ro"
}

func buildMySQLDSN(params Params) (string, string, error) {
	raw := strings.TrimPrefix(params.JDBCUrl, "jdbc:mysql://")
	parsed, err := url.Parse("mysql://" + raw)
	if err != nil || parsed.Host == "" {
		return "", "", fmt.Errorf("无效的 MySQL 连接地址: %s", params.JDBCUrl)
	}

	cfg := mysqldriver.NewConfig()
	cfg.User = params.Username
	cfg.Passwd = params.Password
	cfg.Net = "tcp"
	cfg.Addr = parsed.Host
	cfg.DBName = strings.TrimPrefix(parsed.Path, "/")
	cfg.Timeout = testTimeout
	cfg.ReadTimeout = testTimeout
	cfg.WriteTimeout = testTimeout

	// JDBC 参数不能直接传给 go-sql-driver（会被当作系统变量执行），
	// 连接测试只关心 TLS 语义，其余参数忽略。
	for key, values := range parsed.Query() {
		if len(values) == 0 {
			continue
		}
		switch strings.ToLower(key) {
		case "usessl", "ssl":
			if strings.EqualFold(values[0], "true") {
				cfg.TLSConfig = "skip-verify"
			}
		case "sslmode":
			switch strings.ToLower(values[0]) {
			case "require", "verify-ca", "verify-full", "preferred":
				cfg.TLSConfig = "skip-verify"
			}
		}
	}

	return "mysql", cfg.FormatDSN(), nil
}

func buildPostgresDSN(params Params) (string, string, error) {
	raw := strings.TrimPrefix(params.JDBCUrl, "jdbc:postgresql://")
	parsed, err := url.Parse("postgres://" + raw)
	if err != nil || parsed.Host == "" {
		return "", "", fmt.Errorf("无效的 PostgreSQL 连接地址: %s", params.JDBCUrl)
	}

	if params.Username != "" {
		parsed.User = url.UserPassword(params.Username, params.Password)
	}

	// 只映射 pgx 认识的参数，避免 JDBC 专有参数导致连接失败。
	mapped := url.Values{}
	for key, values := range parsed.Query() {
		if len(values) == 0 {
			continue
		}
		switch strings.ToLower(key) {
		case "ssl":
			if strings.EqualFold(values[0], "true") {
				mapped.Set("sslmode", "require")
			}
		case "sslmode":
			mapped.Set("sslmode", values[0])
		case "currentschema":
			mapped.Set("search_path", values[0])
		}
	}
	mapped.Set("connect_timeout", "5")
	parsed.RawQuery = mapped.Encode()

	return "pgx", parsed.String(), nil
}
