package dbdsn

import (
	"net/url"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestMySQLMapsJDBCParameters(t *testing.T) {
	dsn, err := MySQL(
		"jdbc:mysql://localhost:3306/app?useSSL=true&serverTimezone=UTC&characterEncoding=utf8",
		"app_user",
		"P@ss:w/ord",
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("MySQL() failed: %v", err)
	}
	for _, unwanted := range []string{"useSSL", "serverTimezone", "characterEncoding"} {
		if strings.Contains(dsn, unwanted) {
			t.Errorf("JDBC parameter %s leaked into DSN: %s", unwanted, dsn)
		}
	}
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN() failed: %v", err)
	}
	if parsed.User != "app_user" || parsed.Passwd != "P@ss:w/ord" ||
		parsed.Addr != "localhost:3306" || parsed.DBName != "app" {
		t.Errorf("parsed config = %+v", parsed)
	}
	if parsed.TLSConfig != "skip-verify" || !parsed.ParseTime || parsed.Loc != time.UTC {
		t.Errorf("TLS/parseTime/loc = %q/%v/%v", parsed.TLSConfig, parsed.ParseTime, parsed.Loc)
	}
}

func TestPostgresMapsKnownParameters(t *testing.T) {
	dsn, err := Postgres(
		"jdbc:postgresql://localhost:5432/app?ssl=true&currentSchema=wms&unknownParam=1",
		"app_user",
		"P@ss:w/ord",
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("Postgres() failed: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN is not a URL: %v", err)
	}
	password, _ := parsed.User.Password()
	if parsed.User.Username() != "app_user" || password != "P@ss:w/ord" {
		t.Errorf("credentials = %q/%q", parsed.User.Username(), password)
	}
	if parsed.Query().Get("sslmode") != "require" {
		t.Errorf("ssl=true should map to sslmode=require: %s", dsn)
	}
	if parsed.Query().Get("search_path") != "wms" {
		t.Errorf("currentSchema should map to search_path: %s", dsn)
	}
	if parsed.Query().Get("unknownParam") != "" {
		t.Errorf("unknown parameter was forwarded: %s", dsn)
	}
	if parsed.Query().Get("connect_timeout") != "5" {
		t.Errorf("connect_timeout = %q", parsed.Query().Get("connect_timeout"))
	}
}

func TestMySQLRejectsCertificateVerificationModes(t *testing.T) {
	for _, jdbcURL := range []string{
		"jdbc:mysql://localhost:3306/app?sslMode=VERIFY_CA",
		"jdbc:mysql://localhost:3306/app?sslMode=VERIFY_FULL",
		"jdbc:mysql://localhost:3306/app?sslMode=VERIFY_IDENTITY",
		"jdbc:mysql://localhost:3306/app?useSSL=true&verifyServerCertificate=true",
	} {
		if _, err := MySQL(jdbcURL, "user", "secret", 0); err == nil {
			t.Errorf("MySQL() accepted unsupported verification mode: %s", jdbcURL)
		}
	}
}

func TestRejectsInvalidURLs(t *testing.T) {
	if _, err := MySQL("jdbc:mysql://", "user", "", 0); err == nil {
		t.Error("MySQL() accepted an empty host")
	}
	if _, err := Postgres("jdbc:postgresql://", "user", "", 0); err == nil {
		t.Error("Postgres() accepted an empty host")
	}
}
