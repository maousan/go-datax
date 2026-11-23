package server

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInitializeAuthGeneratesAndPersistsCredentials(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "server.json")
	cfg := DefaultConfig(dataDir)

	generated, err := InitializeAuth(&cfg, configPath)
	if err != nil {
		t.Fatalf("InitializeAuth() failed: %v", err)
	}
	if len(generated.Password) != defaultPasswordChars {
		t.Errorf("generated password length = %d, want %d", len(generated.Password), defaultPasswordChars)
	}
	if !strings.HasPrefix(cfg.Auth.Username, "admin-") {
		t.Errorf("username = %q, want a random admin-* name", cfg.Auth.Username)
	}
	if !strings.HasPrefix(cfg.Auth.Password, "$2") {
		t.Errorf("password is not stored as bcrypt: %q", cfg.Auth.Password)
	}
	if len(cfg.Auth.JWTSecret) < 2*minJWTSecretBytes {
		t.Errorf("jwt secret is too short: %d chars", len(cfg.Auth.JWTSecret))
	}
	if cfg.Auth.TokenTTL != "12h" {
		t.Errorf("tokenTTL = %q, want 12h", cfg.Auth.TokenTTL)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), generated.Password) {
		t.Error("plaintext password was written to the config file")
	}

	reloaded, err := LoadOrCreate(configPath, dataDir)
	if err != nil {
		t.Fatalf("LoadOrCreate() failed: %v", err)
	}
	if reloaded.Auth != cfg.Auth {
		t.Errorf("auth config was not persisted: %+v != %+v", reloaded.Auth, cfg.Auth)
	}
	if !VerifyPassword(reloaded.Auth.Password, generated.Password) {
		t.Error("generated password does not verify against persisted hash")
	}
}

func TestInitializeAuthDoesNotRegenerateExistingCredentials(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "server.json")
	cfg := DefaultConfig(t.TempDir())
	cfg.Auth = AuthConfig{
		Username:  "ops",
		Password:  "plain-password",
		TokenTTL:  "1h",
		JWTSecret: strings.Repeat("s", minJWTSecretBytes),
	}
	want := cfg.Auth

	generated, err := InitializeAuth(&cfg, configPath)
	if err != nil {
		t.Fatalf("InitializeAuth() failed: %v", err)
	}
	if generated.Password != "" || generated.Username != "" {
		t.Errorf("unexpected generated credentials: %+v", generated)
	}
	if cfg.Auth != want {
		t.Errorf("auth config changed: %+v != %+v", cfg.Auth, want)
	}
}

func TestVerifyPasswordSupportsBcryptAndPlaintext(t *testing.T) {
	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword() failed: %v", err)
	}

	cases := []struct {
		name     string
		stored   string
		provided string
		want     bool
	}{
		{"bcrypt match", hash, "s3cret", true},
		{"bcrypt mismatch", hash, "wrong", false},
		{"plaintext match", "plain", "plain", true},
		{"plaintext mismatch", "plain", "wrong", false},
		{"empty stored", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyPassword(tc.stored, tc.provided); got != tc.want {
				t.Errorf("VerifyPassword(%q, %q) = %v, want %v", tc.stored, tc.provided, got, tc.want)
			}
		})
	}
}

func TestTokenServiceIssuesParsableToken(t *testing.T) {
	svc, err := NewTokenService(AuthConfig{
		JWTSecret: strings.Repeat("s", minJWTSecretBytes),
		TokenTTL:  "12h",
	})
	if err != nil {
		t.Fatalf("NewTokenService() failed: %v", err)
	}

	token, session, err := svc.Issue("admin")
	if err != nil {
		t.Fatalf("Issue() failed: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	if time.Until(session.ExpiresAt) <= 11*time.Hour {
		t.Errorf("expiry is too soon: %v", session.ExpiresAt)
	}

	parsed, err := svc.Parse(token)
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if parsed.Username != "admin" {
		t.Errorf("username = %q, want admin", parsed.Username)
	}
	if parsed.ExpiresAt.Unix() != session.ExpiresAt.Unix() {
		t.Errorf("parsed expiry = %v, want %v", parsed.ExpiresAt, session.ExpiresAt)
	}
}

func TestTokenServiceRejectsExpiredToken(t *testing.T) {
	svc, err := NewTokenService(AuthConfig{
		JWTSecret: strings.Repeat("s", minJWTSecretBytes),
		TokenTTL:  "12h",
	})
	if err != nil {
		t.Fatalf("NewTokenService() failed: %v", err)
	}
	svc.now = func() time.Time { return time.Now().Add(-13 * time.Hour) }

	token, _, err := svc.Issue("admin")
	if err != nil {
		t.Fatalf("Issue() failed: %v", err)
	}
	if _, err := svc.Parse(token); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestTokenServiceRejectsTokenFromAnotherSecret(t *testing.T) {
	issuer, _ := NewTokenService(AuthConfig{
		JWTSecret: strings.Repeat("a", minJWTSecretBytes),
		TokenTTL:  "12h",
	})
	verifier, _ := NewTokenService(AuthConfig{
		JWTSecret: strings.Repeat("b", minJWTSecretBytes),
		TokenTTL:  "12h",
	})

	token, _, err := issuer.Issue("admin")
	if err != nil {
		t.Fatalf("Issue() failed: %v", err)
	}
	if _, err := verifier.Parse(token); err == nil {
		t.Fatal("expected token signed with another secret to be rejected")
	}
}

func TestNewTokenServiceValidatesConfig(t *testing.T) {
	cases := []struct {
		name string
		auth AuthConfig
	}{
		{"missing secret", AuthConfig{TokenTTL: "12h"}},
		{"short secret", AuthConfig{JWTSecret: "too-short", TokenTTL: "12h"}},
		{"invalid ttl", AuthConfig{JWTSecret: strings.Repeat("s", minJWTSecretBytes), TokenTTL: "not-a-duration"}},
		{"zero ttl", AuthConfig{JWTSecret: strings.Repeat("s", minJWTSecretBytes), TokenTTL: "0s"}},
		{"negative ttl", AuthConfig{JWTSecret: strings.Repeat("s", minJWTSecretBytes), TokenTTL: "-1h"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewTokenService(tc.auth); err == nil {
				t.Error("expected invalid auth config to fail")
			}
		})
	}
}

func TestConfigFileIsNotWorldReadableAfterSave(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不以 POSIX 权限位表达访问控制")
	}

	path := filepath.Join(t.TempDir(), "server.json")
	if err := Save(path, DefaultConfig(t.TempDir())); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() failed: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("config permissions = %v, want no group/other access", info.Mode().Perm())
	}
}
