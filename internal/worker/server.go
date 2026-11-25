package worker

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const bearerPrefix = "Bearer "

// Server exposes the worker protocol over HTTP.
type Server struct {
	token   string
	manager *runManager
}

// NewServer creates a worker HTTP server. The token must be non-empty.
func NewServer(token string) *Server {
	return &Server{token: token, manager: newRunManager()}
}

// Handler returns the authenticated worker routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "ok",
			"protocol": ProtocolVersion,
		})
	})
	mux.Handle("POST /v1/runs", s.auth(http.HandlerFunc(s.handleStart)))
	mux.Handle("GET /v1/runs/{id}", s.auth(http.HandlerFunc(s.handleStatus)))
	mux.Handle("POST /v1/runs/{id}/cancel", s.auth(http.HandlerFunc(s.handleCancel)))
	mux.Handle("GET /v1/runs/{id}/events", s.auth(http.HandlerFunc(s.handleEvents)))
	return mux
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, bearerPrefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(header, bearerPrefix)), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid worker token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var spec RunSpec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid run spec")
		return
	}
	id, err := s.manager.Start(spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_spec", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	state, ok := s.manager.Status(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if !s.manager.Cancel(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	events, live, done, ok := s.manager.Subscribe(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	for _, event := range events {
		if err := writeSSE(w, event); err != nil {
			return
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
	if done {
		return
	}

	for {
		select {
		case event, ok := <-live:
			if !ok {
				return
			}
			if err := writeSSE(w, event); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSE(w http.ResponseWriter, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
	return err
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

type managedRun struct {
	spec   RunSpec
	state  RunState
	cancel context.CancelFunc

	events []Event
	subs   map[chan Event]struct{}
	done   bool
}

type runManager struct {
	mu   sync.Mutex
	runs map[string]*managedRun
}

func newRunManager() *runManager {
	return &runManager{runs: map[string]*managedRun{}}
}

func (m *runManager) Start(spec RunSpec) (string, error) {
	spec, err := normalizeRunSpec(spec)
	if err != nil {
		return "", err
	}
	id, err := newRunID()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &managedRun{
		spec: spec,
		state: RunState{
			ID:        id,
			Status:    StatusQueued,
			StartedAt: time.Now(),
		},
		cancel: cancel,
		subs:   map[chan Event]struct{}{},
	}
	m.mu.Lock()
	m.runs[id] = run
	m.mu.Unlock()

	go m.execute(ctx, id, spec)
	return id, nil
}

func (m *runManager) execute(ctx context.Context, id string, spec RunSpec) {
	m.publish(id, Event{Type: "log", Message: "diagnostic run started"})
	m.setStatus(id, StatusRunning)
	sink := &countingSink{}
	_, _ = RunDiagnostic(ctx, spec, sink, func(event Event) {
		m.publish(id, event)
	})
}

func (m *runManager) setStatus(id string, status RunStatus) {
	m.mu.Lock()
	if run := m.runs[id]; run != nil {
		run.state.Status = status
	}
	m.mu.Unlock()
}

func (m *runManager) publish(id string, event Event) {
	m.mu.Lock()
	run := m.runs[id]
	if run == nil {
		m.mu.Unlock()
		return
	}
	event.RunID = id
	run.events = append(run.events, event)
	if event.Type == "progress" {
		run.state.ReadRecords = event.ReadRecords
		run.state.ReadBytes = event.ReadBytes
	}
	if event.Type == "done" {
		run.state.Status = event.Status
		run.state.ReadRecords = event.ReadRecords
		run.state.ReadBytes = event.ReadBytes
		run.state.Error = event.Error
		finished := time.Now()
		run.state.FinishedAt = &finished
		run.done = true
	}
	for ch := range run.subs {
		select {
		case ch <- event:
		default:
		}
	}
	if event.Type == "done" {
		for ch := range run.subs {
			close(ch)
		}
		run.subs = nil
	}
	m.mu.Unlock()
}

func (m *runManager) Status(id string) (RunState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.runs[id]
	if run == nil {
		return RunState{}, false
	}
	state := run.state
	if run.state.FinishedAt != nil {
		finished := *run.state.FinishedAt
		state.FinishedAt = &finished
	}
	return state, true
}

func (m *runManager) Cancel(id string) bool {
	m.mu.Lock()
	run := m.runs[id]
	if run == nil {
		m.mu.Unlock()
		return false
	}
	cancel := run.cancel
	m.mu.Unlock()
	cancel()
	return true
}

func (m *runManager) Subscribe(id string) ([]Event, <-chan Event, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.runs[id]
	if run == nil {
		return nil, nil, false, false
	}
	events := append([]Event(nil), run.events...)
	if run.done {
		return events, nil, true, true
	}
	ch := make(chan Event, 64)
	run.subs[ch] = struct{}{}
	return events, ch, false, true
}

type countingSink struct {
	mu      sync.Mutex
	batches int
	rows    int64
	bytes   int64
}

func (s *countingSink) SendBatch(ctx context.Context, batch Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.batches++
	s.rows += int64(batch.Rows)
	s.bytes += batch.Bytes
	s.mu.Unlock()
	return nil
}

func newRunID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate run id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
