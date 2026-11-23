package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const bearerPrefix = "Bearer "

// Server 组合管理台的全部 HTTP 路由。
type Server struct {
	auth    AuthConfig
	version string
	assets  fs.FS
	tokens  *TokenService
	handler http.Handler
}

// New 创建一个管理台服务。assets 是前端构建产物的根文件系统（index.html 位于根）。
func New(cfg Config, version string, assets fs.FS) (*Server, error) {
	if assets == nil {
		return nil, fmt.Errorf("assets filesystem is required")
	}

	tokens, err := NewTokenService(cfg.Auth)
	if err != nil {
		return nil, err
	}

	s := &Server{
		auth:    cfg.Auth,
		version: version,
		assets:  assets,
		tokens:  tokens,
	}
	s.handler = s.routes()
	return s, nil
}

// Handler 返回可直接用于 http.Server 或 httptest 的处理器。
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/health", methodNotAllowed(http.MethodGet))
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/api/v1/auth/login", methodNotAllowed(http.MethodPost))

	// 受保护子树：后续业务端点只需注册在这里，默认就要求登录。
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/v1/auth/me", s.handleMe)
	protected.HandleFunc("/api/v1/auth/me", methodNotAllowed(http.MethodGet))
	protected.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
	})
	mux.Handle("/api/v1/", s.requireAuth(protected))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
	})
	mux.Handle("/", s.staticHandler())

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": s.version,
	})
}

func (s *Server) staticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}

		if info, err := fs.Stat(s.assets, name); err == nil && !info.IsDir() {
			if name == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// 资源文件缺失就是 404；只有前端路由才回退到入口页面。
		if isAssetRequest(name) {
			http.NotFound(w, r)
			return
		}

		s.serveIndex(w)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter) {
	data, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		// 尚未构建前端：明确失败，而不是用提示页伪装成可用界面。
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(uiNotBuiltPage))
		return
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func isAssetRequest(name string) bool {
	if strings.HasPrefix(name, "assets/") {
		return true
	}

	assetExtensions := map[string]bool{
		".js": true, ".css": true, ".map": true, ".ico": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
		".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
		".json": true, ".txt": true, ".webmanifest": true,
	}
	return assetExtensions[strings.ToLower(path.Ext(name))]
}

func methodNotAllowed(allow string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不支持")
	}
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]apiError{
		"error": {Code: code, Message: message},
	})
}

const uiNotBuiltPage = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>go-datax admin</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 4rem auto; line-height: 1.7;">
<h1>前端尚未构建</h1>
<p>请先构建前端产物，再重新编译或启动服务：</p>
<pre style="background:#f4f4f4;padding:1rem;border-radius:6px;">pnpm --dir web install
pnpm --dir web build</pre>
<p>或直接运行 <code>make build</code>。</p>
</body>
</html>`
