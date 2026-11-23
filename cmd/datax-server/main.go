package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/server"
	"github.com/longkeyy/go-datax/internal/store"
	webassets "github.com/longkeyy/go-datax/web"
)

// Version 由构建时通过 ldflags 注入。
var Version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hash-password" {
		runHashPassword()
		return
	}

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

	// 认证配置先补齐并写盘，再应用命令行覆盖，避免把一次性 flag 持久化。
	generated, err := server.InitializeAuth(&cfg, configPath)
	if err != nil {
		log.Fatalf("初始化认证配置失败: %v", err)
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

	srv, err := server.New(cfg, Version, assets)
	if err != nil {
		log.Fatalf("初始化服务失败: %v", err)
	}

	if generated.Password != "" {
		log.Printf("已生成管理账号 %s，初始密码：%s（仅显示一次，请妥善保存）", cfg.Auth.Username, generated.Password)
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

// runHashPassword 从标准输入读取明文并输出 bcrypt 哈希。
// 不使用命令行参数传递密码，避免留在 shell 历史与进程列表中。
func runHashPassword() {
	info, err := os.Stdin.Stat()
	interactive := err == nil && info.Mode()&os.ModeCharDevice != 0
	if interactive {
		fmt.Fprint(os.Stderr, "请输入密码: ")
	}

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		log.Fatalf("读取密码失败: %v", err)
	}
	plain := strings.TrimRight(line, "\r\n")
	if plain == "" {
		log.Fatal("密码不能为空")
	}

	hash, err := server.HashPassword(plain)
	if err != nil {
		log.Fatalf("生成密码哈希失败: %v", err)
	}
	fmt.Println(hash)
}
