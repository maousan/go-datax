package server

import (
	"encoding/json"
	"net/http"
	"runtime"
	"testing"
)

func TestMonitorEndpoint(t *testing.T) {
	_, handler := newTestServer(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t))
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/monitor", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("monitor status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var snapshot struct {
		IntervalSeconds  int               `json:"intervalSeconds"`
		RetentionSeconds int               `json:"retentionSeconds"`
		GoVersion        string            `json:"goVersion"`
		Current          json.RawMessage   `json:"current"`
		Samples          []json.RawMessage `json:"samples"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("monitor response is not JSON: %v", err)
	}
	if snapshot.IntervalSeconds <= 0 || snapshot.RetentionSeconds < snapshot.IntervalSeconds {
		t.Fatalf("interval/retention = %d/%d", snapshot.IntervalSeconds, snapshot.RetentionSeconds)
	}
	if snapshot.GoVersion == "" {
		t.Fatal("goVersion is empty")
	}
	if len(snapshot.Current) == 0 {
		t.Fatal("current sample is missing")
	}
	if len(snapshot.Samples) == 0 {
		t.Fatal("samples are empty")
	}

	var current struct {
		Cores      int `json:"cores"`
		Goroutines int `json:"goroutines"`
		States     struct {
			Running int `json:"running"`
		} `json:"states"`
	}
	if err := json.Unmarshal(snapshot.Current, &current); err != nil {
		t.Fatalf("decode current sample: %v", err)
	}
	if current.Cores != runtime.NumCPU() {
		t.Fatalf("cores = %d, want %d", current.Cores, runtime.NumCPU())
	}
	if current.Goroutines <= 0 {
		t.Fatalf("goroutines = %d", current.Goroutines)
	}

	unauthorized := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/monitor", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated monitor status = %d, want 401", unauthorized.Code)
	}
}
