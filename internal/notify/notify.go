package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

// SettingKey 是通知配置在 AppSetting 中的键。
const SettingKey = "notifications"

// 通知事件。
const (
	EventFailed    = "failed"
	EventRecovered = "recovered"
	EventSucceeded = "succeeded"
	EventCanceled  = "canceled"
	EventTest      = "test"
)

// 通知类型。
const (
	TypeGeneric  = "generic"
	TypeFeishu   = "feishu"
	TypeDingTalk = "dingtalk"
	TypeWeCom    = "wecom"
	TypeSlack    = "slack"
)

var allowedTypes = map[string]bool{
	TypeGeneric:  true,
	TypeFeishu:   true,
	TypeDingTalk: true,
	TypeWeCom:    true,
	TypeSlack:    true,
}

var allowedEvents = map[string]bool{
	EventFailed:    true,
	EventRecovered: true,
	EventSucceeded: true,
	EventCanceled:  true,
}

// Config 是 Webhook 通知配置。
type Config struct {
	Enabled        bool     `json:"enabled"`
	Type           string   `json:"type"`
	URL            string   `json:"url"`
	Events         []string `json:"events"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
}

// DefaultConfig 返回默认配置：关闭通知，失败与恢复事件。
func DefaultConfig() Config {
	return Config{
		Type:           TypeGeneric,
		Events:         []string{EventFailed, EventRecovered},
		TimeoutSeconds: 10,
	}
}

// Normalize 规范化类型、事件与超时。
func (c Config) Normalize() Config {
	c.Type = strings.ToLower(strings.TrimSpace(c.Type))
	if c.Type == "" {
		c.Type = TypeGeneric
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 10
	}
	if c.Events == nil {
		c.Events = append([]string(nil), DefaultConfig().Events...)
	}

	events := make([]string, 0, len(c.Events))
	seen := make(map[string]bool, len(c.Events))
	for _, event := range c.Events {
		event = strings.ToLower(strings.TrimSpace(event))
		if event == "" || seen[event] {
			continue
		}
		seen[event] = true
		events = append(events, event)
	}
	c.Events = events
	return c
}

// Validate 校验配置。未启用时允许 URL 为空。
func (c Config) Validate() error {
	c = c.Normalize()
	if !allowedTypes[c.Type] {
		return fmt.Errorf("不支持的通知类型: %s", c.Type)
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 60 {
		return fmt.Errorf("通知超时必须在 1 到 60 秒之间")
	}
	for _, event := range c.Events {
		if !allowedEvents[event] {
			return fmt.Errorf("不支持的通知事件: %s", event)
		}
	}
	if !c.Enabled {
		return nil
	}
	if len(c.Events) == 0 {
		return fmt.Errorf("至少选择一个通知事件")
	}
	return validateWebhookURL(c.URL)
}

func validateWebhookURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("Webhook 地址不能为空")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("Webhook 地址必须是 http 或 https URL")
	}
	return nil
}

// Notifier 按配置把运行结果发送到 Webhook。
type Notifier struct {
	store *store.Store

	mu  sync.RWMutex
	cfg Config
}

// New 创建通知器并从管理库加载配置。
func New(dataStore *store.Store) *Notifier {
	n := &Notifier{store: dataStore, cfg: DefaultConfig()}
	n.load()
	return n
}

func (n *Notifier) load() {
	if n.store == nil {
		return
	}
	raw, found, err := n.store.GetSetting(SettingKey)
	if err != nil {
		log.Printf("读取通知设置失败: %v", err)
		return
	}
	if !found || strings.TrimSpace(raw) == "" {
		return
	}

	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		log.Printf("解析通知设置失败: %v", err)
		return
	}
	n.cfg = cfg.Normalize()
}

// Config 返回当前配置的副本。
func (n *Notifier) Config() Config {
	n.mu.RLock()
	defer n.mu.RUnlock()

	cfg := n.cfg
	cfg.Events = append([]string(nil), n.cfg.Events...)
	return cfg
}

// Update 校验并持久化配置，然后立即生效。
func (n *Notifier) Update(cfg Config) error {
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("编码通知配置失败: %w", err)
	}
	if n.store != nil {
		if err := n.store.SaveSetting(SettingKey, string(data)); err != nil {
			return fmt.Errorf("保存通知配置失败: %w", err)
		}
	}

	n.mu.Lock()
	n.cfg = cfg
	n.mu.Unlock()
	return nil
}

// Notify 在运行结束时按配置发送通知。willRetry 为 true 表示失败后还会自动重试。
func (n *Notifier) Notify(run store.JobRun, willRetry bool) {
	cfg := n.Config()
	if !cfg.Enabled {
		return
	}

	event := eventFor(run, willRetry)
	if event == "" || !containsEvent(cfg.Events, event) {
		return
	}

	go func() {
		if err := send(cfg, event, run); err != nil {
			log.Printf("发送通知失败（运行 #%d，事件 %s）: %v", run.ID, event, err)
		}
	}()
}

// SendTest 使用给定配置发送一条测试消息。
func (n *Notifier) SendTest(cfg Config) error {
	cfg = cfg.Normalize()
	if !allowedTypes[cfg.Type] {
		return fmt.Errorf("不支持的通知类型: %s", cfg.Type)
	}
	if cfg.TimeoutSeconds < 1 || cfg.TimeoutSeconds > 60 {
		return fmt.Errorf("通知超时必须在 1 到 60 秒之间")
	}
	if err := validateWebhookURL(cfg.URL); err != nil {
		return err
	}
	return sendTest(cfg)
}

func eventFor(run store.JobRun, willRetry bool) string {
	switch run.Status {
	case store.RunStatusFailed:
		if willRetry {
			return ""
		}
		return EventFailed
	case store.RunStatusCanceled:
		return EventCanceled
	case store.RunStatusSucceeded:
		if run.Trigger == store.RunTriggerRetry {
			return EventRecovered
		}
		return EventSucceeded
	default:
		return ""
	}
}

func containsEvent(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}

type genericPayload struct {
	Event           string  `json:"event"`
	Status          string  `json:"status"`
	JobID           int64   `json:"jobId"`
	JobName         string  `json:"jobName"`
	RunID           int64   `json:"runId"`
	Trigger         string  `json:"trigger"`
	RetryAttempt    int     `json:"retryAttempt"`
	StartedAt       string  `json:"startedAt"`
	FinishedAt      string  `json:"finishedAt,omitempty"`
	DurationSeconds float64 `json:"durationSeconds"`
	ExitCode        *int    `json:"exitCode,omitempty"`
	ErrorSummary    string  `json:"errorSummary,omitempty"`
}

func send(cfg Config, event string, run store.JobRun) error {
	body, err := buildBody(cfg, event, run)
	if err != nil {
		return err
	}
	return post(cfg, body)
}

func buildBody(cfg Config, event string, run store.JobRun) ([]byte, error) {
	if cfg.Type == TypeGeneric {
		return json.Marshal(genericPayload{
			Event:           event,
			Status:          run.Status,
			JobID:           jobID(run),
			JobName:         run.SyncJobName,
			RunID:           run.ID,
			Trigger:         run.Trigger,
			RetryAttempt:    run.RetryAttempt,
			StartedAt:       run.StartedAt.UTC().Format(time.RFC3339),
			FinishedAt:      finishedAt(run),
			DurationSeconds: durationSeconds(run),
			ExitCode:        run.ExitCode,
			ErrorSummary:    truncateRunes(strings.ReplaceAll(run.ErrorSummary, "\n", " "), 500),
		})
	}
	return textBody(cfg.Type, formatText(event, run))
}

func sendTest(cfg Config) error {
	var body []byte
	var err error
	if cfg.Type == TypeGeneric {
		body, err = json.Marshal(map[string]any{
			"event":   EventTest,
			"message": "go-datax 通知测试",
		})
	} else {
		body, err = textBody(cfg.Type, "【go-datax】通知测试\nWebhook 配置可用。")
	}
	if err != nil {
		return err
	}
	return post(cfg, body)
}

func textBody(eventType, text string) ([]byte, error) {
	switch eventType {
	case TypeFeishu:
		return json.Marshal(map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": text},
		})
	case TypeDingTalk, TypeWeCom:
		return json.Marshal(map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": text},
		})
	case TypeSlack:
		return json.Marshal(map[string]string{"text": text})
	default:
		return nil, fmt.Errorf("不支持的通知类型: %s", eventType)
	}
}

func post(cfg Config, body []byte) error {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	req, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造通知请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "go-datax-admin")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求 Webhook 失败: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := truncateRunes(strings.TrimSpace(string(data)), 300)
		return fmt.Errorf("Webhook 返回 HTTP %d: %s", resp.StatusCode, message)
	}
	return responseError(data)
}

func responseError(body []byte) error {
	var result struct {
		ErrCode int    `json:"errcode"`
		Code    int    `json:"code"`
		ErrMsg  string `json:"errmsg"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("Webhook 返回错误: %d %s", result.ErrCode, result.ErrMsg)
	}
	if result.Code != 0 {
		return fmt.Errorf("Webhook 返回错误: %d %s", result.Code, result.Msg)
	}
	return nil
}

func formatText(event string, run store.JobRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【go-datax】%s\n", eventTitle(event))
	fmt.Fprintf(&b, "任务：%s\n", run.SyncJobName)

	trigger := triggerLabel(run.Trigger)
	if run.RetryAttempt > 0 {
		trigger = fmt.Sprintf("%s #%d", trigger, run.RetryAttempt)
	}
	fmt.Fprintf(&b, "运行：#%d · %s\n", run.ID, trigger)
	fmt.Fprintf(&b, "开始：%s\n", formatLocal(run.StartedAt))
	if run.FinishedAt != nil {
		fmt.Fprintf(&b, "结束：%s\n", formatLocal(*run.FinishedAt))
		fmt.Fprintf(&b, "耗时：%s\n", formatDuration(*run.FinishedAt, run.StartedAt))
	}
	if run.ExitCode != nil {
		fmt.Fprintf(&b, "退出码：%d\n", *run.ExitCode)
	}
	if summary := truncateRunes(strings.ReplaceAll(run.ErrorSummary, "\n", " "), 300); summary != "" {
		fmt.Fprintf(&b, "错误：%s\n", summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

func eventTitle(event string) string {
	switch event {
	case EventFailed:
		return "任务失败"
	case EventRecovered:
		return "任务已恢复"
	case EventSucceeded:
		return "任务成功"
	case EventCanceled:
		return "任务已取消"
	case EventTest:
		return "通知测试"
	default:
		return event
	}
}

func triggerLabel(trigger string) string {
	switch trigger {
	case store.RunTriggerManual:
		return "手动触发"
	case store.RunTriggerRerun:
		return "重跑"
	case store.RunTriggerRetry:
		return "自动重试"
	default:
		return trigger
	}
}

func jobID(run store.JobRun) int64 {
	if run.SyncJobID == nil {
		return 0
	}
	return *run.SyncJobID
}

func finishedAt(run store.JobRun) string {
	if run.FinishedAt == nil {
		return ""
	}
	return run.FinishedAt.UTC().Format(time.RFC3339)
}

func durationSeconds(run store.JobRun) float64 {
	if run.FinishedAt == nil {
		return 0
	}
	seconds := run.FinishedAt.Sub(run.StartedAt).Seconds()
	if seconds < 0 {
		return 0
	}
	return seconds
}

func formatLocal(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05")
}

func formatDuration(finished, started time.Time) string {
	seconds := int(finished.Sub(started).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	rest := seconds % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, rest)
	}
	return fmt.Sprintf("%dh %dm", minutes/60, minutes%60)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
