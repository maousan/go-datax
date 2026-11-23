// Package oracledsn 把 Oracle 的 JDBC URL 转换为 go-ora 可用的 DSN。
package oracledsn

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	nativePrefix = "oracle://"
	thinPrefix   = "jdbc:oracle:thin:@"
)

// Build 支持以下输入：
//
//	jdbc:oracle:thin:@host:1521/service
//	jdbc:oracle:thin:@//host:1521/service
//	jdbc:oracle:thin:@host:1521:SID
//	oracle://host:1521/service
//
// 用户名与密码会做 URL 编码后写入 DSN。
func Build(jdbcURL, username, password string) (string, error) {
	raw := strings.TrimSpace(jdbcURL)
	lower := strings.ToLower(raw)

	var hostPart string
	switch {
	case strings.HasPrefix(lower, nativePrefix):
		hostPart = raw[len(nativePrefix):]
		if index := strings.LastIndex(hostPart, "@"); index >= 0 {
			hostPart = hostPart[index+1:]
		}
	case strings.HasPrefix(lower, thinPrefix):
		hostPart = raw[len(thinPrefix):]
		if strings.Contains(hostPart, "@") {
			return "", fmt.Errorf("connection URL must not embed credentials; use the username and password fields")
		}
	default:
		return "", fmt.Errorf("invalid Oracle JDBC URL: %s", jdbcURL)
	}

	hostPort, service, err := splitHostService(hostPart)
	if err != nil {
		return "", err
	}

	target := &url.URL{
		Scheme: "oracle",
		Host:   hostPort,
		Path:   "/" + service,
	}
	if username != "" {
		target.User = url.UserPassword(username, password)
	}
	return target.String(), nil
}

func splitHostService(value string) (string, string, error) {
	hostPart := strings.TrimSpace(value)
	hostPart = strings.TrimPrefix(hostPart, "//")
	hostPart = strings.TrimPrefix(hostPart, "/")
	if hostPart == "" {
		return "", "", fmt.Errorf("invalid Oracle JDBC URL: missing host")
	}

	if index := strings.Index(hostPart, "/"); index >= 0 {
		host := strings.TrimSpace(hostPart[:index])
		service := strings.TrimSpace(hostPart[index+1:])
		if host == "" || service == "" {
			return "", "", fmt.Errorf("invalid Oracle JDBC URL: missing host or service")
		}
		return host, service, nil
	}

	// host:port:SID 形式，最后一个冒号作为分隔符。
	if strings.Count(hostPart, ":") >= 2 {
		index := strings.LastIndex(hostPart, ":")
		host := strings.TrimSpace(hostPart[:index])
		service := strings.TrimSpace(hostPart[index+1:])
		if host == "" || service == "" {
			return "", "", fmt.Errorf("invalid Oracle JDBC URL: missing host or SID")
		}
		return host, service, nil
	}

	return "", "", fmt.Errorf("service name or SID is required in Oracle JDBC URL")
}
