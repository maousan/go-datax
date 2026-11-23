package oracledsn

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildThinServiceURL(t *testing.T) {
	dsn, err := Build(
		"jdbc:oracle:thin:@db.example.com:1521/ORCLPDB1",
		"app_user",
		"P@ss:w/ord",
	)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN is not a URL: %v", err)
	}
	if parsed.Scheme != "oracle" || parsed.Host != "db.example.com:1521" {
		t.Errorf("unexpected DSN: %s", dsn)
	}
	password, _ := parsed.User.Password()
	if parsed.User.Username() != "app_user" || password != "P@ss:w/ord" {
		t.Errorf("credentials were not preserved: %s", dsn)
	}
	if parsed.Path != "/ORCLPDB1" {
		t.Errorf("service = %q, want /ORCLPDB1", parsed.Path)
	}
	if strings.Contains(dsn, "P@ss:w/ord") {
		t.Errorf("DSN contains an unescaped password: %s", dsn)
	}
}

func TestBuildThinSlashURL(t *testing.T) {
	dsn, err := Build("jdbc:oracle:thin:@//db.example.com:1521/ORCLPDB1", "app_user", "secret")
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if !strings.HasPrefix(dsn, "oracle://app_user:secret@db.example.com:1521/ORCLPDB1") {
		t.Errorf("unexpected DSN: %s", dsn)
	}
}

func TestBuildThinSIDURL(t *testing.T) {
	dsn, err := Build("jdbc:oracle:thin:@db.example.com:1521:ORCL", "app_user", "secret")
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if !strings.HasSuffix(dsn, "/ORCL") {
		t.Errorf("SID was not converted to a path: %s", dsn)
	}
}

func TestBuildNativeURL(t *testing.T) {
	dsn, err := Build("oracle://db.example.com:1521/ORCLPDB1", "app_user", "secret")
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if !strings.HasPrefix(dsn, "oracle://app_user:secret@db.example.com:1521/ORCLPDB1") {
		t.Errorf("unexpected DSN: %s", dsn)
	}
}

func TestBuildRejectsInvalidURLs(t *testing.T) {
	cases := []string{
		"jdbc:mysql://localhost/db",
		"jdbc:oracle:thin:@",
		"jdbc:oracle:thin:@db.example.com:1521",
		"jdbc:oracle:thin:@app_user:secret@db.example.com:1521/ORCL",
	}

	for _, jdbcURL := range cases {
		if _, err := Build(jdbcURL, "user", "pass"); err == nil {
			t.Errorf("Build(%q) = nil error, want failure", jdbcURL)
		}
	}
}
