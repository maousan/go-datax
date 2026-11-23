package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type authSessionKey struct{}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}

	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "用户名和密码不能为空")
		return
	}

	if !s.auth.Verify(req.Username, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	}

	token, session, err := s.tokens.Issue(req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "签发登录令牌失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token":     token,
		"expiresAt": session.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	session, ok := r.Context().Value(authSessionKey{}).(Session)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"username":  session.Username,
		"expiresAt": session.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, bearerPrefix) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
			return
		}

		raw := strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix))
		session, err := s.tokens.Parse(raw)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新登录")
			return
		}

		ctx := context.WithValue(r.Context(), authSessionKey{}, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
