package alert

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/notify"
	"github.com/longkeyy/go-datax/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "alert.db"))
	if err != nil {
		t.Fatalf("store.Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	return dataStore
}

func sampleRun() store.JobRun {
	jobID := int64(7)
	started := time.Date(2026, 11, 24, 10, 0, 0, 0, time.Local)
	finished := started.Add(90 * time.Second)
	exitCode := 3
	return store.JobRun{
		ID:           42,
		SyncJobID:    &jobID,
		SyncJobName:  "nightly-sync",
		Status:       store.RunStatusFailed,
		Trigger:      store.RunTriggerManual,
		StartedAt:    started,
		FinishedAt:   &finished,
		ExitCode:     &exitCode,
		ErrorSummary: "boom\nat line 1",
	}
}

func webhookChannel(rawURL string) Channel {
	return Channel{
		Name:    "ops-webhook",
		Type:    TypeWebhook,
		Enabled: true,
		Events:  []string{notify.EventFailed},
		URL:     rawURL,
	}
}

func waitForLog(t *testing.T, dataStore *store.Store) store.AlertLog {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		logs, _, err := dataStore.ListAlertLogs(store.AlertLogFilter{Page: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("ListAlertLogs() failed: %v", err)
		}
		if len(logs) > 0 {
			return logs[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("alert log was not written")
	return store.AlertLog{}
}

func TestConfigNormalizeAndValidate(t *testing.T) {
	emailChannel := Channel{
		Name:         "ops-mail",
		Type:         TypeEmail,
		Enabled:      true,
		Events:       []string{notify.EventFailed},
		SMTPHost:     "smtp.example.com",
		SMTPUsername: "alert@example.com",
		SMTPPassword: "secret",
		From:         "alert@example.com",
		To:           []string{"ops@example.com"},
		UseTLS:       true,
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "default is valid",
			cfg:  DefaultConfig(),
		},
		{
			name: "webhook",
			cfg: Config{
				Enabled:        true,
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels:       []Channel{webhookChannel("https://example.com/hook")},
			},
		},
		{
			name: "email",
			cfg: Config{
				Enabled:        true,
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels:       []Channel{emailChannel},
			},
		},
		{
			name: "invalid type",
			cfg: Config{
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels:       []Channel{{Name: "sms", Type: "sms", Events: []string{notify.EventFailed}}},
			},
			wantErr: true,
		},
		{
			name: "missing webhook url",
			cfg: Config{
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels:       []Channel{{Name: "hook", Type: TypeWebhook, Events: []string{notify.EventFailed}}},
			},
			wantErr: true,
		},
		{
			name: "invalid webhook scheme",
			cfg: Config{
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels:       []Channel{webhookChannel("ftp://example.com/hook")},
			},
			wantErr: true,
		},
		{
			name: "empty events",
			cfg: Config{
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels:       []Channel{{Name: "hook", Type: TypeWebhook, Events: []string{}, URL: "https://example.com"}},
			},
			wantErr: true,
		},
		{
			name: "unknown event",
			cfg: Config{
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels:       []Channel{{Name: "hook", Type: TypeWebhook, Events: []string{"exploded"}, URL: "https://example.com"}},
			},
			wantErr: true,
		},
		{
			name: "duplicate names",
			cfg: Config{
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels: []Channel{
					webhookChannel("https://example.com/one"),
					webhookChannel("https://example.com/two"),
				},
			},
			wantErr: true,
		},
		{
			name: "email without recipients",
			cfg: Config{
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels: []Channel{{
					Name: "mail", Type: TypeEmail, Events: []string{notify.EventFailed},
					SMTPHost: "smtp.example.com", From: "alert@example.com",
				}},
			},
			wantErr: true,
		},
		{
			name: "email with invalid recipient",
			cfg: Config{
				TimeoutSeconds: 10,
				RetentionDays:  90,
				Channels: []Channel{{
					Name: "mail", Type: TypeEmail, Events: []string{notify.EventFailed},
					SMTPHost: "smtp.example.com", From: "alert@example.com",
					To: []string{"not-an-address"},
				}},
			},
			wantErr: true,
		},
		{
			name: "timeout out of range",
			cfg: Config{
				TimeoutSeconds: 61,
				RetentionDays:  90,
			},
			wantErr: true,
		},
		{
			name: "retention out of range",
			cfg: Config{
				TimeoutSeconds: 10,
				RetentionDays:  0,
			},
			wantErr: false, // Normalize 会补默认值
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg.Normalize()
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			for _, channel := range cfg.Channels {
				if channel.ID == "" && !tt.wantErr {
					t.Errorf("channel %q has no ID after Normalize()", channel.Name)
				}
			}
		})
	}

	normalized := Config{Channels: []Channel{emailChannel}}.Normalize()
	if normalized.TimeoutSeconds != defaultTimeoutSeconds || normalized.RetentionDays != defaultRetentionDays {
		t.Errorf("defaults = %d/%d", normalized.TimeoutSeconds, normalized.RetentionDays)
	}
	if normalized.Channels[0].SMTPPort != 465 {
		t.Errorf("default SMTP port = %d, want 465", normalized.Channels[0].SMTPPort)
	}
}

func TestMigrateLegacyNotificationSetting(t *testing.T) {
	dataStore := openTestStore(t)
	legacy := map[string]any{
		"enabled":        true,
		"type":           notify.TypeDingTalk,
		"url":            "https://example.com/dingtalk",
		"events":         []string{notify.EventFailed, notify.EventRecovered},
		"timeoutSeconds": 5,
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy setting: %v", err)
	}
	if err := dataStore.SaveSetting("notifications", string(data)); err != nil {
		t.Fatalf("SaveSetting() failed: %v", err)
	}

	manager := New(dataStore)
	cfg := manager.Config()
	if !cfg.Enabled || cfg.TimeoutSeconds != 5 || len(cfg.Channels) != 1 {
		t.Fatalf("migrated config = %+v", cfg)
	}
	channel := cfg.Channels[0]
	if channel.Type != TypeDingTalk || channel.URL != legacy["url"] || !channel.Enabled {
		t.Errorf("migrated channel = %+v", channel)
	}
	if len(channel.Events) != 2 {
		t.Errorf("migrated events = %v", channel.Events)
	}
	if cfg.RetentionDays != defaultRetentionDays {
		t.Errorf("retention = %d, want %d", cfg.RetentionDays, defaultRetentionDays)
	}
}

func TestNotifyDispatchesAndLogs(t *testing.T) {
	dataStore := openTestStore(t)

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		requests <- string(body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	manager := New(dataStore)
	if err := manager.Update(Config{
		Enabled:        true,
		TimeoutSeconds: 5,
		RetentionDays:  90,
		Channels:       []Channel{webhookChannel(server.URL)},
	}); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	manager.Notify(sampleRun(), false)

	select {
	case body := <-requests:
		if !strings.Contains(body, `"event":"failed"`) {
			t.Errorf("payload = %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook did not receive a request")
	}

	entry := waitForLog(t, dataStore)
	if entry.Status != store.AlertStatusSuccess {
		t.Errorf("log status = %q, want success", entry.Status)
	}
	if entry.Event != notify.EventFailed || entry.ChannelName != "ops-webhook" || entry.ChannelType != TypeWebhook {
		t.Errorf("log = %+v", entry)
	}
	if entry.RunID != 42 || entry.SyncJobName != "nightly-sync" || entry.SyncJobID == nil || *entry.SyncJobID != 7 {
		t.Errorf("log run fields = %+v", entry)
	}
}

func TestNotifySkipsUnsubscribedEvents(t *testing.T) {
	dataStore := openTestStore(t)

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.String()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	manager := New(dataStore)
	if err := manager.Update(Config{
		Enabled:        true,
		TimeoutSeconds: 5,
		RetentionDays:  90,
		Channels:       []Channel{webhookChannel(server.URL)},
	}); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// 成功事件未订阅。
	run := sampleRun()
	run.Status = store.RunStatusSucceeded
	manager.Notify(run, false)

	// 失败但会自动重试，不发送。
	manager.Notify(sampleRun(), true)

	select {
	case target := <-requests:
		t.Fatalf("unexpected alert request: %s", target)
	case <-time.After(300 * time.Millisecond):
	}

	logs, total, err := dataStore.ListAlertLogs(store.AlertLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListAlertLogs() failed: %v", err)
	}
	if total != 0 || len(logs) != 0 {
		t.Errorf("unexpected logs = %+v", logs)
	}
}

func TestTestSendFailureIsLogged(t *testing.T) {
	dataStore := openTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	manager := New(dataStore)
	channel := webhookChannel(server.URL)
	err := manager.Test(channel, 5)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("Test() error = %v, want HTTP 500", err)
	}

	entry := waitForLog(t, dataStore)
	if entry.Status != store.AlertStatusFailed || entry.Event != notify.EventTest {
		t.Errorf("log = %+v", entry)
	}
	if !strings.Contains(entry.Error, "HTTP 500") {
		t.Errorf("log error = %q", entry.Error)
	}
}

func TestSecretsAreMaskedAndPreserved(t *testing.T) {
	dataStore := openTestStore(t)

	manager := New(dataStore)
	if err := manager.Update(Config{
		Enabled:        true,
		TimeoutSeconds: 10,
		RetentionDays:  90,
		Channels: []Channel{{
			Name:         "ops-mail",
			Type:         TypeEmail,
			Enabled:      true,
			Events:       []string{notify.EventFailed},
			SMTPHost:     "smtp.example.com",
			SMTPPort:     465,
			SMTPUsername: "alert@example.com",
			SMTPPassword: "smtp-secret",
			From:         "alert@example.com",
			To:           []string{"ops@example.com"},
			UseTLS:       true,
		}},
	}); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	masked := manager.Config()
	if masked.Channels[0].SMTPPassword != MaskedSecret {
		t.Errorf("masked password = %q", masked.Channels[0].SMTPPassword)
	}
	if masked.Channels[0].SMTPPassword == "smtp-secret" {
		t.Fatal("plain password leaked through Config()")
	}

	masked.Channels[0].Name = "ops-mail-renamed"
	if err := manager.Update(masked); err != nil {
		t.Fatalf("Update() with masked secret failed: %v", err)
	}
	if got := manager.snapshot().Channels[0].SMTPPassword; got != "smtp-secret" {
		t.Errorf("stored password = %q, want preserved secret", got)
	}

	reloaded := New(dataStore)
	if got := reloaded.snapshot().Channels[0].SMTPPassword; got != "smtp-secret" {
		t.Errorf("reloaded password = %q, want preserved secret", got)
	}
}

func TestDingTalkSignature(t *testing.T) {
	type captured struct {
		query url.Values
		body  map[string]any
	}
	got := make(chan captured, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- captured{query: r.URL.Query(), body: body}
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()

	channel := Channel{
		Name:    "dingtalk",
		Type:    TypeDingTalk,
		Enabled: true,
		Events:  []string{notify.EventFailed},
		URL:     server.URL,
		Secret:  "SECxxxxxxxx",
	}
	if err := send(channel, notify.EventTest, store.JobRun{}, 5*time.Second); err != nil {
		t.Fatalf("send() failed: %v", err)
	}

	result := <-got
	if result.query.Get("timestamp") == "" || result.query.Get("sign") == "" {
		t.Errorf("missing signature query: %v", result.query)
	}
	if result.body["msgtype"] != "text" {
		t.Errorf("body = %+v", result.body)
	}
}

func TestFeishuSignature(t *testing.T) {
	body := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		body <- payload
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()

	channel := Channel{
		Name:    "feishu",
		Type:    TypeFeishu,
		Enabled: true,
		Events:  []string{notify.EventFailed},
		URL:     server.URL,
		Secret:  "feishu-secret",
	}
	if err := send(channel, notify.EventTest, store.JobRun{}, 5*time.Second); err != nil {
		t.Fatalf("send() failed: %v", err)
	}

	payload := <-body
	if payload["msg_type"] != "text" {
		t.Errorf("msg_type = %v", payload["msg_type"])
	}
	if payload["timestamp"] == nil || payload["sign"] == nil {
		t.Errorf("missing signature fields: %+v", payload)
	}
}

func startFakeSMTP(t *testing.T) (string, int, <-chan string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen SMTP: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	messages := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		writer := bufio.NewWriter(conn)
		reader := textproto.NewReader(bufio.NewReader(conn))
		reply := func(line string) {
			_, _ = writer.WriteString(line + "\r\n")
			_ = writer.Flush()
		}
		reply("220 fake ESMTP")

		for {
			line, err := reader.ReadLine()
			if err != nil {
				return
			}
			command := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
				reply("250-fake")
				reply("250-AUTH PLAIN")
				reply("250 HELP")
			case strings.HasPrefix(command, "AUTH"):
				reply("235 2.7.0 Authentication successful")
			case strings.HasPrefix(command, "MAIL FROM"):
				reply("250 2.1.0 OK")
			case strings.HasPrefix(command, "RCPT TO"):
				reply("250 2.1.5 OK")
			case strings.HasPrefix(command, "DATA"):
				reply("354 End data with <CR><LF>.<CR><LF>")
				var message strings.Builder
				for {
					dataLine, err := reader.ReadLine()
					if err != nil {
						return
					}
					if dataLine == "." {
						break
					}
					message.WriteString(dataLine + "\r\n")
				}
				messages <- message.String()
				reply("250 2.0.0 OK")
			case strings.HasPrefix(command, "QUIT"):
				reply("221 2.0.0 Bye")
				return
			default:
				reply("250 OK")
			}
		}
	}()

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split SMTP address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse SMTP port: %v", err)
	}
	return host, port, messages
}

func TestSendEmail(t *testing.T) {
	host, port, messages := startFakeSMTP(t)

	channel := Channel{
		Name:         "ops-mail",
		Type:         TypeEmail,
		Enabled:      true,
		Events:       []string{notify.EventFailed},
		SMTPHost:     host,
		SMTPPort:     port,
		SMTPUsername: "alert@example.com",
		SMTPPassword: "smtp-secret",
		From:         "alert@example.com",
		To:           []string{"ops@example.com"},
		UseTLS:       false,
	}
	if err := send(channel, notify.EventFailed, sampleRun(), 5*time.Second); err != nil {
		t.Fatalf("send() failed: %v", err)
	}

	select {
	case message := <-messages:
		for _, want := range []string{
			"From: go-datax <alert@example.com>",
			"To: ops@example.com",
			"Subject: ",
			"Content-Transfer-Encoding: base64",
		} {
			if !strings.Contains(message, want) {
				t.Errorf("mail does not contain %q:\n%s", want, message)
			}
		}
		parts := strings.SplitN(message, "\r\n\r\n", 2)
		if len(parts) != 2 {
			t.Fatalf("mail has no body: %q", message)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(parts[1], "\r\n", ""))
		if err != nil {
			t.Fatalf("decode mail body: %v", err)
		}
		if !strings.Contains(string(decoded), "任务失败") || !strings.Contains(string(decoded), "nightly-sync") {
			t.Errorf("decoded body = %q", decoded)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP server did not receive a message")
	}
}

func TestBuildMailEncodesContent(t *testing.T) {
	raw := string(buildMail("alert@example.com", []string{"ops@example.com"}, "【go-datax】任务失败", "第一行\n第二行"))
	for _, want := range []string{
		"From: go-datax <alert@example.com>",
		"To: ops@example.com",
		"Subject: ",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: base64",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("mail does not contain %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "第一行") {
		t.Error("mail body should be base64 encoded")
	}
}

func TestLegacyChannelType(t *testing.T) {
	tests := map[string]string{
		notify.TypeGeneric:  TypeWebhook,
		notify.TypeFeishu:   TypeFeishu,
		notify.TypeDingTalk: TypeDingTalk,
		notify.TypeWeCom:    TypeWeCom,
		notify.TypeSlack:    TypeSlack,
		"":                  TypeWebhook,
		"sms":               TypeWebhook,
	}
	for input, want := range tests {
		if got := legacyChannelType(input); got != want {
			t.Errorf("legacyChannelType(%q) = %q, want %q", input, got, want)
		}
	}
}
