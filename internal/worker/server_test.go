package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func startWorkerServer(t *testing.T) (*httptest.Server, *Client) {
	t.Helper()
	token := "test-worker-token"
	ts := httptest.NewServer(NewServer(token).Handler())
	t.Cleanup(ts.Close)
	return ts, NewClient(ts.URL, token)
}

func TestServerHealthAndAuth(t *testing.T) {
	ts, _ := startWorkerServer(t)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}

	for _, token := range []string{"", "wrong"} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/runs", strings.NewReader(`{"rows":10}`))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("start request: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q status = %d, want 401", token, resp.StatusCode)
		}
	}
}

func TestServerRunStreamsToCompletion(t *testing.T) {
	_, client := startWorkerServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id, err := client.StartRun(ctx, RunSpec{
		Rows:      1024,
		BatchSize: 256,
		Splits:    2,
		MaxBytes:  51200,
	})
	if err != nil {
		t.Fatalf("StartRun() failed: %v", err)
	}

	events, errs := client.StreamEvents(ctx, id)
	var done Event
	for done.Status == "" {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("event stream closed before done")
			}
			if event.Type == "done" {
				done = event
			}
		case err, ok := <-errs:
			if ok && err != nil {
				t.Fatalf("event stream error: %v", err)
			}
			errs = nil
		case <-ctx.Done():
			t.Fatal("timed out waiting for run completion")
		}
	}
	if done.Status != StatusSucceeded {
		t.Fatalf("done status = %q, want succeeded (%s)", done.Status, done.Error)
	}
	if done.ReadRecords != 1024 {
		t.Fatalf("done records = %d, want 1024", done.ReadRecords)
	}
	if len(done.Tasks) != 2 {
		t.Fatalf("task stats = %d, want 2", len(done.Tasks))
	}

	state, err := client.Status(ctx, id)
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if state.Status != StatusSucceeded || state.ReadRecords != 1024 {
		t.Fatalf("state = %+v, want succeeded/1024", state)
	}
	if len(state.Tasks) != 2 {
		t.Fatalf("state task stats = %d, want 2", len(state.Tasks))
	}
}

func TestServerCancelStopsRun(t *testing.T) {
	_, client := startWorkerServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id, err := client.StartRun(ctx, RunSpec{
		Rows:            1000000,
		BatchSize:       256,
		Splits:          2,
		MaxBytes:        51200,
		DelayPerBatchMS: 20,
	})
	if err != nil {
		t.Fatalf("StartRun() failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := client.CancelRun(ctx, id); err != nil {
		t.Fatalf("CancelRun() failed: %v", err)
	}

	events, errs := client.StreamEvents(ctx, id)
	var done Event
	for done.Status == "" {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("event stream closed before done")
			}
			if event.Type == "done" {
				done = event
			}
		case err, ok := <-errs:
			if ok && err != nil {
				t.Fatalf("event stream error: %v", err)
			}
			errs = nil
		case <-ctx.Done():
			t.Fatal("timed out waiting for cancellation")
		}
	}
	if done.Status != StatusCanceled {
		t.Fatalf("done status = %q, want canceled (%s)", done.Status, done.Error)
	}
}
