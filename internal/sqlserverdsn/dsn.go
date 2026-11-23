// Package sqlserverdsn 把 SQL Server 的 JDBC URL 转换为 go-mssqldb 可用的 DSN。
package sqlserverdsn

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	sqlServerPrefix = "jdbc:sqlserver://"
	jtdsPrefix      = "jdbc:jtds:sqlserver://"
)

// allowedParameters 只保留 go-mssqldb 认识的参数，避免 jTDS 专有参数导致连接失败。
var allowedParameters = map[string]bool{
	"encrypt":               true,
	"trustservercertificate": true,
	"tlsmin":                true,
	"connection timeout":    true,
	"dial timeout":          true,
	"keepalive":             true,
	"packet size":           true,
	"app name":              true,
	"applicationintent":     true,
	"failoverpartner":       true,
	"instance":              true,
	"multisubnetfailover":   true,
}

// Build 支持 Microsoft JDBC 与 jTDS 两种 URL 形式。
// 用户名与密码会做 URL 编码，避免 @、: 等字符破坏 DSN。
func Build(jdbcURL, username, password string) (string, error) {
	var rest string
	switch {
	case strings.HasPrefix(strings.ToLower(jdbcURL), sqlServerPrefix):
		rest = jdbcURL[len(sqlServerPrefix):]
	case strings.HasPrefix(strings.ToLower(jdbcURL), jtdsPrefix):
		rest = jdbcURL[len(jtdsPrefix):]
	default:
		return "", fmt.Errorf("invalid SQL Server JDBC URL: %s", jdbcURL)
	}

	parts := strings.Split(rest, ";")
	target := strings.TrimSpace(parts[0])
	if target == "" {
		return "", fmt.Errorf("invalid SQL Server JDBC URL: missing host")
	}
	if strings.Contains(target, "@") {
		return "", fmt.Errorf("connection URL must not embed credentials; use the username and password fields")
	}

	hostPort := target
	pathDatabase := ""
	if index := strings.Index(target, "/"); index >= 0 {
		hostPort = strings.TrimSpace(target[:index])
		pathDatabase = strings.TrimSpace(target[index+1:])
	}
	if hostPort == "" {
		return "", fmt.Errorf("invalid SQL Server JDBC URL: missing host")
	}

	params := url.Values{}
	database := ""
	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		value := strings.TrimSpace(kv[1])
		if key == "" {
			continue
		}

		switch key {
		case "databasename", "database":
			database = value
		case "user", "username", "password":
			// 凭据以显式参数为准，忽略 URL 中的副本。
			continue
		default:
			if allowedParameters[key] {
				params.Set(key, value)
			}
		}
	}

	if database == "" {
		database = pathDatabase
	}
	if database == "" {
		return "", fmt.Errorf("DatabaseName is required in JDBC URL")
	}
	params.Set("database", database)

	targetURL := &url.URL{
		Scheme:   "sqlserver",
		Host:     hostPort,
		RawQuery: params.Encode(),
	}
	if username != "" {
		targetURL.User = url.UserPassword(username, password)
	}

	return targetURL.String(), nil
}
