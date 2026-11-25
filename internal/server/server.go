package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/longkeyy/go-datax/internal/alert"
	"github.com/longkeyy/go-datax/internal/execution"
	"github.com/longkeyy/go-datax/internal/monitor"
	"github.com/longkeyy/go-datax/internal/store"
)

const bearerPrefix = "Bearer "

// Server 组合管理台的全部 HTTP 路由。
type Server struct {
	config       Config
	configPath   string
	version      string
	assets       fs.FS
	authMu       sync.RWMutex
	tokens       *TokenService
	dataStore    *store.Store
	executor     *execution.Manager
	alerts       *alert.Manager
	scheduler    *collectScheduler
	worker       *workerSupervisor
	monitor      *monitor.Sampler
	dataxVersion string
	handler      http.Handler
}

// New 创建一个管理台服务。assets 是前端构建产物的根文件系统（index.html 位于根）。
func New(cfg Config, version string, assets fs.FS, dataStore *store.Store, executors ...*execution.Manager) (*Server, error) {
	if assets == nil {
		return nil, fmt.Errorf("assets filesystem is required")
	}
	if dataStore == nil {
		return nil, fmt.Errorf("data store is required")
	}

	tokens, err := NewTokenService(cfg.Auth)
	if err != nil {
		return nil, err
	}

	s := &Server{
		config:     cfg,
		configPath: cfg.path,
		version:    version,
		assets:     assets,
		tokens:     tokens,
		dataStore:  dataStore,
		alerts:     alert.New(dataStore),
	}
	for _, executor := range executors {
		if executor != nil {
			s.executor = executor
			s.dataxVersion = executor.DataXVersion()
			executor.SetOnFinish(s.alerts.Notify)
		}
	}
	s.scheduler = newCollectScheduler(s)
	s.worker = newWorkerSupervisor(cfg.Worker, dataStore)
	s.monitor = monitor.New(monitor.DefaultInterval, monitor.DefaultRetention)
	s.monitor.Start()
	s.applySavedRuntimeSettings()

	s.handler = s.routes()
	return s, nil
}

// StartScheduler 启动采集任务调度循环。
func (s *Server) StartScheduler() {
	if s.scheduler != nil {
		s.scheduler.Start()
	}
}

// StopScheduler 停止采集任务调度循环。
func (s *Server) StopScheduler() {
	if s.scheduler != nil {
		s.scheduler.Stop()
	}
}

// StartWorker 启动本地 etl-worker，或连接配置里的外部 worker。
func (s *Server) StartWorker() error {
	if s.worker == nil {
		return nil
	}
	return s.worker.Start()
}

// StopWorker 停止 etl-worker 监管循环。
func (s *Server) StopWorker() {
	if s.worker != nil {
		s.worker.Stop()
	}
}

func (s *Server) applySavedRuntimeSettings() {
	if s.executor == nil {
		return
	}
	raw, found, err := s.dataStore.GetSetting(runtimeSettingKey)
	if err != nil || !found || strings.TrimSpace(raw) == "" {
		return
	}
	var settings execution.RuntimeSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		log.Printf("忽略无法解析的运行参数设置: %v", err)
		return
	}
	if err := validateRuntimeSettings(settings); err != nil {
		log.Printf("忽略无效的运行参数设置: %v", err)
		return
	}
	s.executor.UpdateRuntime(settings)
}

// authSnapshot 返回当前认证配置与令牌服务的快照。
func (s *Server) authSnapshot() (AuthConfig, *TokenService) {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.config.Auth, s.tokens
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

	protected.HandleFunc("GET /api/v1/data-sources", s.handleListDataSources)
	protected.HandleFunc("POST /api/v1/data-sources", s.handleCreateDataSource)
	protected.HandleFunc("POST /api/v1/data-sources/test", s.handleTestDataSource)
	protected.HandleFunc("GET /api/v1/data-sources/{id}", s.handleGetDataSource)
	protected.HandleFunc("PUT /api/v1/data-sources/{id}", s.handleUpdateDataSource)
	protected.HandleFunc("DELETE /api/v1/data-sources/{id}", s.handleDeleteDataSource)
	protected.HandleFunc("GET /api/v1/data-sources/{id}/schemas", s.handleDataSourceSchemas)
	protected.HandleFunc("GET /api/v1/data-sources/{id}/tables", s.handleDataSourceTables)
	protected.HandleFunc("GET /api/v1/data-sources/{id}/columns", s.handleDataSourceColumns)
	protected.HandleFunc("GET /api/v1/data-sources/{id}/rows", s.handleDataSourceRows)
	protected.HandleFunc("POST /api/v1/data-sources/{id}/query", s.handleDataSourceQuery)

	s.registerEndpointRoutes(protected, store.EndpointKindSource, "/api/v1/sources")
	s.registerEndpointRoutes(protected, store.EndpointKindSink, "/api/v1/sinks")

	protected.HandleFunc("GET /api/v1/datax/datasets", s.handleListQueryTemplates)
	protected.HandleFunc("POST /api/v1/datax/datasets", s.handleCreateQueryTemplate)
	protected.HandleFunc("/api/v1/datax/datasets", methodNotAllowed("GET, POST"))
	protected.HandleFunc("GET /api/v1/datax/datasets/{id}", s.handleGetQueryTemplate)
	protected.HandleFunc("PUT /api/v1/datax/datasets/{id}", s.handleUpdateQueryTemplate)
	protected.HandleFunc("DELETE /api/v1/datax/datasets/{id}", s.handleDeleteQueryTemplate)
	protected.HandleFunc("/api/v1/datax/datasets/{id}", methodNotAllowed("GET, PUT, DELETE"))

	protected.HandleFunc("GET /api/v1/datax/sync-jobs", s.handleListSyncJobs)
	protected.HandleFunc("POST /api/v1/datax/sync-jobs", s.handleCreateSyncJob)
	protected.HandleFunc("POST /api/v1/datax/sync-jobs/import", s.handleImportSyncJob)
	protected.HandleFunc("GET /api/v1/datax/sync-jobs/{id}", s.handleGetSyncJob)
	protected.HandleFunc("PUT /api/v1/datax/sync-jobs/{id}", s.handleUpdateSyncJob)
	protected.HandleFunc("DELETE /api/v1/datax/sync-jobs/{id}", s.handleDeleteSyncJob)
	protected.HandleFunc("GET /api/v1/datax/sync-jobs/{id}/stats", s.handleSyncJobStats)
	protected.HandleFunc("/api/v1/datax/sync-jobs/{id}/stats", methodNotAllowed(http.MethodGet))
	protected.HandleFunc("POST /api/v1/datax/sync-jobs/{id}/duplicate", s.handleDuplicateSyncJob)
	protected.HandleFunc("GET /api/v1/datax/sync-jobs/{id}/export", s.handleExportSyncJob)
	protected.HandleFunc("POST /api/v1/datax/sync-jobs/{id}/runs", s.handleStartRun)

	protected.HandleFunc("GET /api/v1/datax/collect-tasks", s.handleListCollectTasks)
	protected.HandleFunc("POST /api/v1/datax/collect-tasks", s.handleCreateCollectTask)
	protected.HandleFunc("/api/v1/datax/collect-tasks", methodNotAllowed("GET, POST"))
	protected.HandleFunc("GET /api/v1/datax/collect-tasks/{id}", s.handleGetCollectTask)
	protected.HandleFunc("PUT /api/v1/datax/collect-tasks/{id}", s.handleUpdateCollectTask)
	protected.HandleFunc("DELETE /api/v1/datax/collect-tasks/{id}", s.handleDeleteCollectTask)
	protected.HandleFunc("/api/v1/datax/collect-tasks/{id}", methodNotAllowed("GET, PUT, DELETE"))
	protected.HandleFunc("POST /api/v1/datax/collect-tasks/{id}/run", s.handleRunCollectTask)
	protected.HandleFunc("/api/v1/datax/collect-tasks/{id}/run", methodNotAllowed(http.MethodPost))

	protected.HandleFunc("GET /api/v1/datax/runs", s.handleListRuns)
	protected.HandleFunc("GET /api/v1/datax/runs/{id}", s.handleGetRun)
	protected.HandleFunc("POST /api/v1/datax/runs/{id}/cancel", s.handleCancelRun)
	protected.HandleFunc("POST /api/v1/datax/runs/{id}/rerun", s.handleRerun)
	protected.HandleFunc("/api/v1/datax/runs/{id}/rerun", methodNotAllowed(http.MethodPost))
	protected.HandleFunc("GET /api/v1/datax/runs/{id}/logs/stream", s.handleRunLogStream)
	protected.HandleFunc("GET /api/v1/datax/runs/{id}/log", s.handleRunLogDownload)
	protected.HandleFunc("GET /api/v1/datax/runs/{id}/snapshot", s.handleRunSnapshot)

	protected.HandleFunc("GET /api/v1/overview", s.handleOverview)
	protected.HandleFunc("GET /api/v1/monitor", s.handleMonitor)

	protected.HandleFunc("GET /api/v1/alerts/config", s.handleGetAlertConfig)
	protected.HandleFunc("PUT /api/v1/alerts/config", s.handleUpdateAlertConfig)
	protected.HandleFunc("/api/v1/alerts/config", methodNotAllowed("GET, PUT"))
	protected.HandleFunc("POST /api/v1/alerts/config/test", s.handleTestAlertChannel)
	protected.HandleFunc("/api/v1/alerts/config/test", methodNotAllowed(http.MethodPost))
	protected.HandleFunc("GET /api/v1/alerts/logs", s.handleListAlertLogs)
	protected.HandleFunc("DELETE /api/v1/alerts/logs", s.handleClearAlertLogs)
	protected.HandleFunc("/api/v1/alerts/logs", methodNotAllowed("GET, DELETE"))

	protected.HandleFunc("GET /api/v1/settings/account", s.handleGetAccountSettings)
	protected.HandleFunc("PUT /api/v1/settings/account", s.handleUpdateAccountSettings)
	protected.HandleFunc("/api/v1/settings/account", methodNotAllowed("GET, PUT"))
	protected.HandleFunc("GET /api/v1/settings/runtime", s.handleGetRuntimeSettings)
	protected.HandleFunc("PUT /api/v1/settings/runtime", s.handleUpdateRuntimeSettings)
	protected.HandleFunc("/api/v1/settings/runtime", methodNotAllowed("GET, PUT"))

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
		"status":       "ok",
		"version":      s.version,
		"dataxVersion": s.dataxVersion,
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
