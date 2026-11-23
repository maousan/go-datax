package sqlserverdsn

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildMicrosoftURL(t *testing.T) {
	dsn, err := Build(
		"jdbc:sqlserver://db.example.com:1433;DatabaseName=sample_db;encrypt=true",
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
	if parsed.Scheme != "sqlserver" {
		t.Errorf("scheme = %q, want sqlserver", parsed.Scheme)
	}
	if parsed.Host != "db.example.com:1433" {
		t.Errorf("host = %q, want db.example.com:1433", parsed.Host)
	}
	if parsed.User.Username() != "app_user" {
		t.Errorf("username = %q, want app_user", parsed.User.Username())
	}
	password, _ := parsed.User.Password()
	if password != "P@ss:w/ord" {
		t.Errorf("password = %q, want P@ss:w/ord", password)
	}
	if got := parsed.Query().Get("database"); got != "sample_db" {
		t.Errorf("database = %q, want sample_db", got)
	}
	if got := parsed.Query().Get("encrypt"); got != "true" {
		t.Errorf("encrypt = %q, want true", got)
	}
	if strings.Contains(dsn, "P@ss:w/ord") {
		t.Errorf("DSN contains an unescaped password: %s", dsn)
	}
}

func TestBuildJTDSUrlWithPathDatabase(t *testing.T) {
	dsn, err := Build(
		"jdbc:jtds:sqlserver://db.example.com:1433/sample_db",
		"app_user",
		"secret",
	)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if !strings.HasPrefix(dsn, "sqlserver://app_user:secret@db.example.com:1433") {
		t.Errorf("unexpected DSN: %s", dsn)
	}
	if !strings.Contains(dsn, "database=sample_db") {
		t.Errorf("DSN missing database: %s", dsn)
	}
}

func TestBuildJTDSUrlWithDatabaseNameParameter(t *testing.T) {
	dsn, err := Build(
		"jdbc:jtds:sqlserver://db.example.com:1433;DatabaseName=sample_db",
		"app_user",
		"secret",
	)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if !strings.Contains(dsn, "database=sample_db") {
		t.Errorf("DSN missing database: %s", dsn)
	}
}

func TestBuildDropsUnknownJTdsParameters(t *testing.T) {
	dsn, err := Build(
		"jdbc:jtds:sqlserver://db.example.com:1433/sample_db;sendStringParametersAsUnicode=false;encrypt=disable",
		"app_user",
		"secret",
	)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if strings.Contains(dsn, "sendStringParametersAsUnicode") {
		t.Errorf("unknown parameter was forwarded: %s", dsn)
	}
	if !strings.Contains(dsn, "encrypt=disable") {
		t.Errorf("known parameter was dropped: %s", dsn)
	}
}

func TestBuildRejectsInvalidURLs(t *testing.T) {
	cases := []string{
		"jdbc:mysql://localhost/db",
		"jdbc:sqlserver://",
		"jdbc:sqlserver://localhost:1433",
		"jdbc:sqlserver://localhost:1433;encrypt=true",
	}

	for _, jdbcURL := range cases {
		if _, err := Build(jdbcURL, "user", "pass"); err == nil {
			t.Errorf("Build(%q) = nil error, want failure", jdbcURL)
		}
	}
}

func TestBuildIgnoresCredentialsInParameters(t *testing.T) {
	dsn, err := Build(
		"jdbc:sqlserver://localhost:1433;DatabaseName=app;user=embedded;password=embedded",
		"explicit",
		"secret",
	)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	parsed, _ := url.Parse(dsn)
	if parsed.User.Username() != "explicit" {
		t.Errorf("username = %q, want explicit", parsed.User.Username())
	}
	if parsed.Query().Get("user") != "" || parsed.Query().Get("password") != "" {
		t.Errorf("URL credentials leaked into DSN: %s", dsn)
	}
}

func TestBuildRejectsEmbeddedUserInfo(t *testing.T) {
	if _, err := Build(
		"jdbc:sqlserver://embedded:secret@localhost:1433;DatabaseName=app",
		"explicit",
		"secret",
	); err == nil {
		t.Error("expected embedded userinfo to be rejected")
	}
}
