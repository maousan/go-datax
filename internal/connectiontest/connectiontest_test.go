package connectiontest

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createSQLiteFile(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE probe (id INTEGER)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
}

func TestSQLiteConnectionSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	createSQLiteFile(t, dbPath)

	result := Test(context.Background(), Params{
		Type:    "sqlite",
		JDBCUrl: "jdbc:sqlite:" + dbPath,
	})
	if !result.OK {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.LatencyMs < 0 {
		t.Errorf("latency = %d, want >= 0", result.LatencyMs)
	}
	if result.Version == "" {
		t.Error("expected sqlite version to be reported")
	}
}

func TestSQLiteConnectionTestDoesNotCreateFiles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing.db")

	result := Test(context.Background(), Params{
		Type:    "sqlite",
		JDBCUrl: "jdbc:sqlite:" + dbPath,
	})
	if result.OK {
		t.Fatal("expected missing sqlite database to fail")
	}
	if _, err := os.Stat(dbPath); err == nil {
		t.Error("connection test created the sqlite file")
	}
}

func TestUnsupportedTypeFails(t *testing.T) {
	result := Test(context.Background(), Params{Type: "oracle", JDBCUrl: "jdbc:oracle:thin:@x"})
	if result.OK {
		t.Fatal("expected failure for unsupported type")
	}
	if result.Message == "" {
		t.Error("expected an error message")
	}
}

func TestUnreachableMySQLFails(t *testing.T) {
	result := Test(context.Background(), Params{
		Type:     "mysql",
		JDBCUrl:  "jdbc:mysql://127.0.0.1:1/app",
		Username: "app_user",
	})
	if result.OK {
		t.Fatal("expected connection failure")
	}
	if result.Message == "" {
		t.Error("expected an error message")
	}
}

func TestConnectionTestTimesOut(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		<-done
		_ = conn.Close()
	}()

	previous := testTimeout
	testTimeout = 200 * time.Millisecond
	defer func() {
		testTimeout = previous
		close(done)
		_ = listener.Close()
	}()

	result := Test(context.Background(), Params{
		Type:     "mysql",
		JDBCUrl:  "jdbc:mysql://" + listener.Addr().String() + "/app",
		Username: "app_user",
	})
	if result.OK {
		t.Fatal("expected timeout, got success")
	}
}

func TestBuildMySQLDSNDropsJDBCParams(t *testing.T) {
	_, dsn, err := buildDSN(Params{
		Type:     "mysql",
		JDBCUrl:  "jdbc:mysql://localhost:3306/app?useSSL=true&serverTimezone=UTC&characterEncoding=utf8",
		Username: "app_user",
		Password: "P@ss:w/ord",
	})
	if err != nil {
		t.Fatalf("buildDSN() failed: %v", err)
	}
	if strings.Contains(dsn, "useSSL") ||
		strings.Contains(dsn, "serverTimezone") ||
		strings.Contains(dsn, "characterEncoding") {
		t.Errorf("JDBC parameters were forwarded to the Go driver: %s", dsn)
	}
	if !strings.Contains(dsn, "tls=skip-verify") {
		t.Errorf("useSSL=true should map to tls=skip-verify: %s", dsn)
	}
}

func TestBuildPostgresDSNMapsKnownParams(t *testing.T) {
	_, dsn, err := buildDSN(Params{
		Type:     "postgres",
		JDBCUrl:  "jdbc:postgresql://localhost:5432/app?ssl=true&currentSchema=wms&unknownParam=1",
		Username: "app_user",
		Password: "P@ss:w/ord",
	})
	if err != nil {
		t.Fatalf("buildDSN() failed: %v", err)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN is not a URL: %v", err)
	}
	password, _ := parsed.User.Password()
	if password != "P@ss:w/ord" {
		t.Errorf("password = %q, want P@ss:w/ord", password)
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
}

func TestBuildOracleDSNFromThinURL(t *testing.T) {
	driver, dsn, err := buildDSN(Params{
		Type:     "oracle",
		JDBCUrl:  "jdbc:oracle:thin:@//db.example.com:1521/orcl",
		Username: "app_user",
		Password: "P@ss:w<>ord",
	})
	if err != nil {
		t.Fatalf("buildDSN() failed: %v", err)
	}
	if driver != "oracle" {
		t.Errorf("driver = %q, want oracle", driver)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN is not a URL: %v", err)
	}
	if parsed.Host != "db.example.com:1521" || parsed.Path != "/orcl" {
		t.Errorf("unexpected DSN: %s", dsn)
	}
	password, _ := parsed.User.Password()
	if password != "P@ss:w<>ord" {
		t.Errorf("password = %q, want P@ss:w<>ord", password)
	}
}

func TestBuildMSSQLDSNAcceptsJTDS(t *testing.T) {
	driver, dsn, err := buildDSN(Params{
		Type:     "mssql",
		JDBCUrl:  "jdbc:jtds:sqlserver://db.example.com:1433/sample_db",
		Username: "app_user",
		Password: "P@ss:w/ord",
	})
	if err != nil {
		t.Fatalf("buildDSN() failed: %v", err)
	}
	if driver != "sqlserver" {
		t.Errorf("driver = %q, want sqlserver", driver)
	}
	if strings.Contains(dsn, "P@ss:w/ord") {
		t.Errorf("DSN contains an unescaped password: %s", dsn)
	}
	if !strings.Contains(dsn, "database=sample_db") {
		t.Errorf("missing database: %s", dsn)
	}
}
