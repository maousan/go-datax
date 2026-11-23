package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	tokenIssuer          = "datax-server"
	defaultTokenTTL      = "12h"
	minJWTSecretBytes    = 32
	defaultPasswordChars = 24
	passwordAlphabet     = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

// GeneratedAuth 保存首次启动时生成、且只应展示一次的凭据。
type GeneratedAuth struct {
	Username string
	Password string
}

// HashPassword 生成 bcrypt 哈希。
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword 校验 bcrypt 哈希或明文密码。
// 明文形式仅用于兼容手工维护的配置；首次启动生成的密码始终以 bcrypt 存储。
func VerifyPassword(stored, provided string) bool {
	if stored == "" {
		return false
	}

	if strings.HasPrefix(stored, "$2a$") ||
		strings.HasPrefix(stored, "$2b$") ||
		strings.HasPrefix(stored, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(provided)) == nil
	}

	return subtle.ConstantTimeCompare([]byte(stored), []byte(provided)) == 1
}

// Verify 校验用户名与密码。两个比较都会执行，调用方据此返回统一错误。
func (c AuthConfig) Verify(username, password string) bool {
	usernameOK := subtle.ConstantTimeCompare([]byte(username), []byte(c.Username)) == 1
	passwordOK := VerifyPassword(c.Password, password)
	return usernameOK && passwordOK
}

// InitializeAuth 补齐缺失的认证配置并持久化，返回本次生成的凭据。
// 生成的密码以 bcrypt 存储；明文只通过返回值交给调用方打印一次。
func InitializeAuth(cfg *Config, configPath string) (GeneratedAuth, error) {
	var generated GeneratedAuth
	changed := false

	if cfg.Auth.Username == "" {
		username, err := generateUsername()
		if err != nil {
			return generated, err
		}
		cfg.Auth.Username = username
		generated.Username = username
		changed = true
	}

	if cfg.Auth.TokenTTL == "" {
		cfg.Auth.TokenTTL = defaultTokenTTL
		changed = true
	}

	if cfg.Auth.Password == "" {
		password, err := generatePassword(defaultPasswordChars)
		if err != nil {
			return generated, err
		}
		hash, err := HashPassword(password)
		if err != nil {
			return generated, err
		}
		cfg.Auth.Password = hash
		generated.Password = password
		changed = true
	}

	if cfg.Auth.JWTSecret == "" {
		secret, err := generateSecret(minJWTSecretBytes)
		if err != nil {
			return generated, err
		}
		cfg.Auth.JWTSecret = secret
		changed = true
	}

	if !changed {
		return GeneratedAuth{}, nil
	}
	if err := Save(configPath, *cfg); err != nil {
		return generated, err
	}
	return generated, nil
}

func generateUsername() (string, error) {
	suffix, err := generateSecret(4)
	if err != nil {
		return "", err
	}
	return "admin-" + suffix, nil
}

func generatePassword(length int) (string, error) {
	buf, err := randomBytes(length)
	if err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = passwordAlphabet[int(buf[i])%len(passwordAlphabet)]
	}
	return string(buf), nil
}

func generateSecret(byteCount int) (string, error) {
	buf, err := randomBytes(byteCount)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomBytes(length int) ([]byte, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}
	return buf, nil
}

// Session 表示一次已通过校验的登录会话。
type Session struct {
	Username  string
	ExpiresAt time.Time
}

// TokenService 负责签发与校验 JWT。
type TokenService struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenService 根据认证配置创建令牌服务。
func NewTokenService(auth AuthConfig) (*TokenService, error) {
	if len(auth.JWTSecret) < minJWTSecretBytes {
		return nil, fmt.Errorf("auth.jwtSecret must be at least %d bytes", minJWTSecretBytes)
	}
	ttl, err := time.ParseDuration(auth.TokenTTL)
	if err != nil {
		return nil, fmt.Errorf("invalid auth.tokenTTL %q: %w", auth.TokenTTL, err)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("auth.tokenTTL must be positive, got %q", auth.TokenTTL)
	}
	return &TokenService{
		secret: []byte(auth.JWTSecret),
		ttl:    ttl,
		now:    time.Now,
	}, nil
}

// Issue 为指定用户签发令牌，返回令牌与会话信息。
func (s *TokenService) Issue(username string) (string, Session, error) {
	now := s.now()
	session := Session{Username: username, ExpiresAt: now.Add(s.ttl)}

	claims := jwt.RegisteredClaims{
		Subject:   username,
		Issuer:    tokenIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(session.ExpiresAt),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", Session{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, session, nil
}

// Parse 校验令牌并返回会话信息。
func (s *TokenService) Parse(raw string) (Session, error) {
	claims := &jwt.RegisteredClaims{}

	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method %q", t.Method.Alg())
		}
		return s.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
	)
	if err != nil {
		return Session{}, fmt.Errorf("invalid token: %w", err)
	}
	if !token.Valid || claims.Subject == "" || claims.ExpiresAt == nil {
		return Session{}, fmt.Errorf("invalid token")
	}

	return Session{Username: claims.Subject, ExpiresAt: claims.ExpiresAt.Time}, nil
}
