package server

import (
	"net/http"
	"strings"
)

type updateAccountRequest struct {
	Username        string `json:"username"`
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleGetAccountSettings(w http.ResponseWriter, r *http.Request) {
	auth, _ := s.authSnapshot()
	writeJSON(w, http.StatusOK, map[string]string{"username": auth.Username})
}

// handleUpdateAccountSettings 修改用户名或密码。保存成功后轮换 JWT 密钥，所有旧令牌立即失效。
func (s *Server) handleUpdateAccountSettings(w http.ResponseWriter, r *http.Request) {
	var req updateAccountRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	if req.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "当前密码不能为空")
		return
	}

	s.authMu.Lock()
	defer s.authMu.Unlock()

	current := s.config.Auth
	if !VerifyPassword(current.Password, req.CurrentPassword) {
		// 当前密码错误属于表单校验，返回 400；避免前端把它当成登录态失效而强制退出。
		writeError(w, http.StatusBadRequest, "invalid_credentials", "当前密码不正确")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = current.Username
	}
	if len([]rune(username)) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_username", "用户名不能超过 100 个字符")
		return
	}
	if strings.ContainsAny(username, "\r\n\t") {
		writeError(w, http.StatusBadRequest, "invalid_username", "用户名不能包含控制字符")
		return
	}

	newPassword := req.NewPassword
	if newPassword != "" {
		if len([]rune(newPassword)) < 8 {
			writeError(w, http.StatusBadRequest, "weak_password", "新密码至少 8 个字符")
			return
		}
		if len([]rune(newPassword)) > 200 {
			writeError(w, http.StatusBadRequest, "weak_password", "新密码不能超过 200 个字符")
			return
		}
	}
	if username == current.Username && newPassword == "" {
		writeError(w, http.StatusBadRequest, "no_changes", "没有需要修改的内容")
		return
	}

	password := current.Password
	if newPassword != "" {
		hash, err := HashPassword(newPassword)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "生成密码哈希失败")
			return
		}
		password = hash
	}

	secret, err := generateSecret(minJWTSecretBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "生成令牌密钥失败")
		return
	}

	updated := current
	updated.Username = username
	updated.Password = password
	updated.JWTSecret = secret

	tokens, err := NewTokenService(updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "更新令牌服务失败")
		return
	}
	if s.configPath == "" {
		writeError(w, http.StatusInternalServerError, "config_unavailable", "配置文件路径未知，无法保存账号修改")
		return
	}

	next := s.config
	next.Auth = updated
	if err := Save(s.configPath, next); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", "保存配置失败："+err.Error())
		return
	}

	s.config = next
	s.tokens = tokens
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "relogin": true})
}
