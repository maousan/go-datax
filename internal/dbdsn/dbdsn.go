// Package dbdsn converts the management plane's JDBC URLs into Go driver DSNs.
package dbdsn

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// MySQL builds a go-sql-driver DSN from a JDBC MySQL URL.
func MySQL(jdbcURL, username, password string, timeout time.Duration) (string, error) {
	raw, ok := trimJDBCPrefix(jdbcURL, "jdbc:mysql://")
	if !ok {
		return "", fmt.Errorf("无效的 MySQL 连接地址: %s", jdbcURL)
	}
	parsed, err := url.Parse("mysql://" + raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("无效的 MySQL 连接地址: %s", jdbcURL)
	}

	cfg := mysqldriver.NewConfig()
	cfg.User = username
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = parsed.Host
	cfg.DBName = strings.TrimPrefix(parsed.Path, "/")
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if timeout > 0 {
		// Only bound connection establishment. ReadTimeout is a socket I/O
		// deadline and would abort legitimate long-running scans.
		cfg.Timeout = timeout
	}

	// JDBC parameters cannot be passed to go-sql-driver directly (they would be
	// executed as system variables), so only TLS semantics are mapped.
	verifyCertificate := false
	for key, values := range parsed.Query() {
		if len(values) == 0 {
			continue
		}
		switch strings.ToLower(key) {
		case "usessl", "ssl":
			// Connector/J's useSSL only requires encryption; it verifies the
			// server certificate only when verifyServerCertificate=true.
			if strings.EqualFold(values[0], "true") {
				cfg.TLSConfig = "skip-verify"
			}
		case "verifyservercertificate":
			verifyCertificate = strings.EqualFold(values[0], "true")
		case "sslmode":
			switch strings.ReplaceAll(strings.ToLower(values[0]), "_", "-") {
			case "require", "required", "preferred":
				cfg.TLSConfig = "skip-verify"
			case "verify-ca", "verify-full", "verify-identity":
				return "", fmt.Errorf("MySQL sslMode=%s requires a trusted CA and is not supported", values[0])
			}
		}
	}
	if verifyCertificate {
		return "", fmt.Errorf("MySQL verifyServerCertificate=true requires a trusted CA and is not supported")
	}
	return cfg.FormatDSN(), nil
}

// Postgres builds a pgx DSN from a JDBC PostgreSQL URL.
func Postgres(jdbcURL, username, password string, timeout time.Duration) (string, error) {
	raw, ok := trimJDBCPrefix(jdbcURL, "jdbc:postgresql://")
	if !ok {
		return "", fmt.Errorf("无效的 PostgreSQL 连接地址: %s", jdbcURL)
	}
	parsed, err := url.Parse("postgres://" + raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("无效的 PostgreSQL 连接地址: %s", jdbcURL)
	}
	if username != "" {
		parsed.User = url.UserPassword(username, password)
	}

	// Only forward parameters that pgx understands.
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
	if timeout > 0 {
		seconds := int(timeout / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		mapped.Set("connect_timeout", fmt.Sprintf("%d", seconds))
	}
	parsed.RawQuery = mapped.Encode()
	return parsed.String(), nil
}

func trimJDBCPrefix(jdbcURL, prefix string) (string, bool) {
	if len(jdbcURL) < len(prefix) || !strings.EqualFold(jdbcURL[:len(prefix)], prefix) {
		return "", false
	}
	return jdbcURL[len(prefix):], true
}
