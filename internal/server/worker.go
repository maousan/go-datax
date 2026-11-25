package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/longkeyy/go-datax/internal/worker"
)

// workerSupervisor owns the etl-worker process and its protocol client.
type workerSupervisor struct {
	cfg     WorkerConfig
	backoff time.Duration

	mu       sync.Mutex
	client   *worker.Client
	cmd      *exec.Cmd
	procDone chan struct{}
	url      string
	stopping bool
	restarts int
	active   map[string]worker.RunStatus
}

func newWorkerSupervisor(cfg WorkerConfig) *workerSupervisor {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:0"
	}
	return &workerSupervisor{
		cfg:     cfg,
		backoff: time.Second,
		active:  map[string]worker.RunStatus{},
	}
}

// Start connects to an external worker or spawns and supervises a local one.
func (s *workerSupervisor) Start() error {
	s.mu.Lock()
	if s.client != nil {
		s.mu.Unlock()
		return nil
	}
	s.stopping = false
	s.mu.Unlock()

	if s.cfg.URL != "" {
		if s.cfg.Token == "" {
			return fmt.Errorf("worker.token is required when worker.url is set")
		}
		client := worker.NewClient(s.cfg.URL, s.cfg.Token)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Health(ctx); err != nil {
			return fmt.Errorf("connect worker at %s: %w", s.cfg.URL, err)
		}
		s.mu.Lock()
		s.client = client
		s.url = s.cfg.URL
		s.mu.Unlock()
		return nil
	}

	if err := s.spawn(); err != nil {
		return err
	}
	go s.supervise()
	return nil
}

// Stop terminates the spawned worker, or drops an external connection.
func (s *workerSupervisor) Stop() {
	s.mu.Lock()
	s.stopping = true
	cmd := s.cmd
	done := s.procDone
	s.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}

	s.mu.Lock()
	s.client = nil
	s.cmd = nil
	s.procDone = nil
	s.url = ""
	s.mu.Unlock()
}

// StartDiagnosticRun submits a diagnostic run to the current worker.
func (s *workerSupervisor) StartDiagnosticRun(ctx context.Context, spec worker.RunSpec) (string, error) {
	client, err := s.currentClient()
	if err != nil {
		return "", err
	}
	id, err := client.StartRun(ctx, spec)
	if err != nil {
		return "", err
	}
	s.setActive(id, worker.StatusRunning)
	return id, nil
}

// WaitRun streams a run until it finishes and records its terminal state.
func (s *workerSupervisor) WaitRun(ctx context.Context, id string) (worker.RunStatus, error) {
	client, err := s.currentClient()
	if err != nil {
		return worker.StatusFailed, err
	}
	events, errs := client.StreamEvents(ctx, id)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				if ctx.Err() != nil {
					return s.RunStatus(id), ctx.Err()
				}
				statusCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				state, statusErr := client.Status(statusCtx, id)
				cancel()
				if statusErr == nil {
					s.setActive(id, state.Status)
					return state.Status, nil
				}
				s.setActive(id, worker.StatusFailed)
				return worker.StatusFailed, statusErr
			}
			if event.Type == "done" {
				s.setActive(id, event.Status)
				return event.Status, nil
			}
		case streamErr, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if streamErr != nil {
				s.setActive(id, worker.StatusFailed)
				return worker.StatusFailed, streamErr
			}
		case <-ctx.Done():
			return s.RunStatus(id), ctx.Err()
		}
	}
}

// RunStatus returns the last known status of a diagnostic run.
func (s *workerSupervisor) RunStatus(id string) worker.RunStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[id]
}

// RestartCount reports how many times the local worker was restarted.
func (s *workerSupervisor) RestartCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restarts
}

// URL returns the current worker base URL.
func (s *workerSupervisor) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.url
}

func (s *workerSupervisor) currentClient() (*worker.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, fmt.Errorf("etl-worker is not available")
	}
	return s.client, nil
}

func (s *workerSupervisor) setActive(id string, status worker.RunStatus) {
	s.mu.Lock()
	s.active[id] = status
	s.mu.Unlock()
}

func (s *workerSupervisor) spawn() error {
	bin, err := resolveWorkerBin(s.cfg.Bin)
	if err != nil {
		return err
	}
	token := s.cfg.Token
	if token == "" {
		token, err = generateWorkerToken()
		if err != nil {
			return err
		}
	}

	cmd := exec.Command(bin, "--listen", s.cfg.Listen)
	cmd.Env = append(os.Environ(), "ETL_WORKER_TOKEN="+token)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("worker stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("worker stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start etl-worker: %w", err)
	}

	urlCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go scanWorkerOutput(stdout, urlCh, errCh)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("etl-worker: %s", scanner.Text())
		}
	}()

	var baseURL string
	select {
	case baseURL = <-urlCh:
	case err := <-errCh:
		_ = cmd.Process.Kill()
		return err
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		return fmt.Errorf("timed out waiting for etl-worker to listen")
	}

	client := worker.NewClient(baseURL, token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Health(ctx); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("etl-worker health check: %w", err)
	}

	done := make(chan struct{})
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		_ = cmd.Process.Kill()
		return fmt.Errorf("worker supervisor stopped")
	}
	s.client = client
	s.cmd = cmd
	s.procDone = done
	s.url = baseURL
	s.mu.Unlock()

	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return nil
}

func (s *workerSupervisor) supervise() {
	for {
		s.mu.Lock()
		done := s.procDone
		stopping := s.stopping
		s.mu.Unlock()
		if done == nil || stopping {
			return
		}
		<-done

		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		for id, status := range s.active {
			if status == worker.StatusQueued || status == worker.StatusRunning {
				s.active[id] = worker.StatusFailed
			}
		}
		s.restarts++
		attempt := s.restarts
		s.client = nil
		s.cmd = nil
		s.procDone = nil
		s.url = ""
		s.mu.Unlock()

		log.Printf("etl-worker 退出，准备重启（第 %d 次）", attempt)
		delay := s.backoff * time.Duration(attempt)
		if delay > 10*time.Second {
			delay = 10 * time.Second
		}
		time.Sleep(delay)

		s.mu.Lock()
		stopping = s.stopping
		s.mu.Unlock()
		if stopping {
			return
		}

		for {
			if err := s.spawn(); err != nil {
				log.Printf("重启 etl-worker 失败: %v", err)
				time.Sleep(s.backoff)
				s.mu.Lock()
				stopping = s.stopping
				s.mu.Unlock()
				if stopping {
					return
				}
				continue
			}
			s.mu.Lock()
			stopping = s.stopping
			cmd := s.cmd
			s.mu.Unlock()
			if stopping {
				if cmd != nil && cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				return
			}
			break
		}
	}
}

func (s *workerSupervisor) killProcess() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func scanWorkerOutput(r io.Reader, urlCh chan<- string, errCh chan<- error) {
	scanner := bufio.NewScanner(r)
	sent := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !sent && strings.HasPrefix(line, "etl-worker listening on ") {
			urlCh <- strings.TrimPrefix(line, "etl-worker listening on ")
			sent = true
			continue
		}
		if line != "" {
			log.Printf("etl-worker: %s", line)
		}
	}
	if !sent {
		err := scanner.Err()
		if err == nil {
			err = errors.New("etl-worker exited before reporting its address")
		}
		select {
		case errCh <- err:
		default:
		}
	}
}

func resolveWorkerBin(configured string) (string, error) {
	var candidates []string
	if configured != "" {
		candidates = append(candidates, configured)
	}
	if executable, err := os.Executable(); err == nil {
		name := "etl-worker"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), name))
	}
	candidates = append(candidates, "etl-worker")

	for _, candidate := range candidates {
		if strings.ContainsAny(candidate, `/\`) {
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				return candidate, nil
			}
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("找不到 etl-worker 可执行文件，请通过 worker.bin 配置或放到 etl-server 同目录")
}

func generateWorkerToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate worker token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
