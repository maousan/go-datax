package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/longkeyy/go-datax/internal/server"
	"github.com/longkeyy/go-datax/internal/store"
	webassets "github.com/longkeyy/go-datax/web"
)

// Version 由构建时通过 ldflags 注入。
var Version = "dev"

func main() {
	var (
		configPath  string
		listenAddr  string
		dataDir     string
		showVersion bool
	)

	flag.StringVar(&configPath, "config", "", "配置文件路径（默认 <data-dir>/server.json）")
	flag.StringVar(&listenAddr, "listen", "", "监听地址（默认 127.0.0.1:8080）")
	flag.StringVar(&dataDir, "data-dir", "", "数据目录")
	flag.BoolVar(&showVersion, "version", false, "显示版本信息")
	flag.Parse()

	if showVersion {
		fmt.Printf("DataX Admin Server %s\n", Version)
		return
	}

	resolvedDataDir, err := resolveDataDir(dataDir)
	if err != nil {
		log.Fatalf("解析数据目录失败: %v", err)
	}
	if configPath == "" {
		configPath = filepath.Join(resolvedDataDir, "server.json")
	}

	cfg, err := server.LoadOrCreate(configPath, resolvedDataDir)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	cfg = cfg.WithOverrides(listenAddr, dataDir)

	if err := server.EnsureDataDirs(cfg); err != nil {
		log.Fatalf("准备数据目录失败: %v", err)
	}
	if err := store.EnsureDatabase(cfg.DatabasePath()); err != nil {
		log.Fatalf("初始化管理库失败: %v", err)
	}

	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		log.Fatalf("加载内嵌前端资源失败: %v", err)
	}

	srv, err := server.New(Version, assets)
	if err != nil {
		log.Fatalf("初始化服务失败: %v", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("datax-server %s 监听 http://%s（数据目录：%s）", Version, cfg.Listen, cfg.DataDir)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("服务启动失败: %v", err)
	}
}

func resolveDataDir(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "go-datax-admin"), nil
}
