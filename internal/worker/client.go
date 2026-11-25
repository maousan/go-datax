package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client talks to a worker over its local HTTP protocol.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a worker client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{},
	}
}

// Health checks the worker's unauthenticated health endpoint.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("worker health: HTTP %d: %s", resp.StatusCode, readBody(resp.Body))
	}
	var health struct {
		Status   string `json:"status"`
		Protocol int    `json:"protocol"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("decode worker health: %w", err)
	}
	if health.Status != "ok" {
		return fmt.Errorf("worker health status = %q", health.Status)
	}
	if health.Protocol != ProtocolVersion {
		return fmt.Errorf("worker protocol = %d, want %d", health.Protocol, ProtocolVersion)
	}
	return nil
}

// StartRun submits a diagnostic or pipeline run and returns its id.
func (c *Client) StartRun(ctx context.Context, spec RunSpec) (string, error) {
	payload, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/runs", strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("start worker run: HTTP %d: %s", resp.StatusCode, readBody(resp.Body))
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode worker run id: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("worker returned an empty run id")
	}
	return result.ID, nil
}

// Status returns the current state of a run.
func (c *Client) Status(ctx context.Context, id string) (RunState, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(id), nil)
	if err != nil {
		return RunState{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return RunState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RunState{}, fmt.Errorf("worker status: HTTP %d: %s", resp.StatusCode, readBody(resp.Body))
	}
	var state RunState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return RunState{}, fmt.Errorf("decode worker status: %w", err)
	}
	return state, nil
}

// CancelRun asks the worker to cancel a run.
func (c *Client) CancelRun(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(id)+"/cancel", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("cancel worker run: HTTP %d: %s", resp.StatusCode, readBody(resp.Body))
	}
	return nil
}

// StreamEvents opens the SSE stream for a run. The event channel closes when the
// stream ends; the error channel receives at most one terminal transport error.
func (c *Client) StreamEvents(ctx context.Context, id string) (<-chan Event, <-chan error) {
	events := make(chan Event, 64)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)

		req, err := c.newRequest(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(id)+"/events", nil)
		if err != nil {
			errs <- err
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			errs <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			errs <- fmt.Errorf("worker events: HTTP %d: %s", resp.StatusCode, readBody(resp.Body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		eventType := ""
		var data strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			case line == "":
				if data.Len() > 0 {
					var event Event
					if err := json.Unmarshal([]byte(data.String()), &event); err != nil {
						errs <- fmt.Errorf("decode worker event: %w", err)
						return
					}
					if event.Type == "" {
						event.Type = eventType
					}
					select {
					case events <- event:
					case <-ctx.Done():
						return
					}
				}
				eventType = ""
				data.Reset()
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}()
	return events, errs
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func readBody(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, 2048))
	return strings.TrimSpace(string(data))
}
