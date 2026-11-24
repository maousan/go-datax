// Package alert 负责告警渠道配置、事件分发与告警日志。
package alert

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/longkeyy/go-datax/internal/notify"
	"github.com/longkeyy/go-datax/internal/store"
)

// SettingKey 是告警配置在 AppSetting 中的键。
const SettingKey = "alerts"

// legacySettingKey 是旧版单 Webhook 通知设置的键，仅用于自动迁移。
const legacySettingKey = "notifications"

// MaskedSecret 是返回给前端时替换口令与签名密钥的占位符。
const MaskedSecret = "******"

// 告警渠道类型。
const (
	TypeEmail    = "email"
	TypeDingTalk = "dingtalk"
	TypeFeishu   = "feishu"
	TypeWeCom    = "wecom"
	TypeSlack    = "slack"
	TypeWebhook  = "webhook"
)

const (
	defaultTimeoutSeconds = 10
	defaultRetentionDays  = 90
	maxChannels           = 50
	maxRecipients         = 50
)

var allowedTypes = map[string]bool{
	TypeEmail:    true,
	TypeDingTalk: true,
	TypeFeishu:   true,
	TypeWeCom:    true,
	TypeSlack:    true,
	TypeWebhook:  true,
}

var allowedEvents = map[string]bool{
	notify.EventFailed:    true,
	notify.EventRecovered: true,
	notify.EventSucceeded: true,
	notify.EventCanceled:  true,
}

var typeLabels = map[string]string{
	TypeEmail:    "邮箱",
	TypeDingTalk: "钉钉",
	TypeFeishu:   "飞书",
	TypeWeCom:    "企业微信",
	TypeSlack:    "Slack",
	TypeWebhook:  "通用 Webhook",
}

// Channel 是一个告警渠道。不同类型使用不同字段：
// 邮箱使用 SMTP 字段，其余类型使用 URL（钉钉、飞书还可选 Secret 签名）。
type Channel struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Enabled bool     `json:"enabled"`
	Events  []string `json:"events"`

	URL    string `json:"url,omitempty"`
	Secret string `json:"secret,omitempty"`

	SMTPHost     string   `json:"smtpHost,omitempty"`
	SMTPPort     int      `json:"smtpPort,omitempty"`
	SMTPUsername string   `json:"smtpUsername,omitempty"`
	SMTPPassword string   `json:"smtpPassword,omitempty"`
	From         string   `json:"from,omitempty"`
	To           []string `json:"to,omitempty"`
	UseTLS       bool     `json:"useTLS,omitempty"`
}

// Config 是完整的告警配置。
type Config struct {
	Enabled        bool      `json:"enabled"`
	TimeoutSeconds int       `json:"timeoutSeconds"`
	RetentionDays  int       `json:"retentionDays"`
	Channels       []Channel `json:"channels"`
}

// DefaultConfig 返回默认配置：关闭告警，无渠道。
func DefaultConfig() Config {
	return Config{
		TimeoutSeconds: defaultTimeoutSeconds,
		RetentionDays:  defaultRetentionDays,
		Channels:       []Channel{},
	}
}

// Normalize 规范化渠道字段并补齐默认值。
func (c Config) Normalize() Config {
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = defaultTimeoutSeconds
	}
	if c.RetentionDays <= 0 {
		c.RetentionDays = defaultRetentionDays
	}

	channels := make([]Channel, 0, len(c.Channels))
	used := make(map[string]bool, len(c.Channels))
	for _, channel := range c.Channels {
		channel = channel.normalize()
		for channel.ID == "" || used[channel.ID] {
			channel.ID = newChannelID()
		}
		used[channel.ID] = true
		channels = append(channels, channel)
	}
	c.Channels = channels
	return c
}

// Validate 校验配置。
func (c Config) Validate() error {
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 60 {
		return fmt.Errorf("告警超时必须在 1 到 60 秒之间")
	}
	if c.RetentionDays < 1 || c.RetentionDays > 3650 {
		return fmt.Errorf("日志保留天数必须在 1 到 3650 之间")
	}
	if len(c.Channels) > maxChannels {
		return fmt.Errorf("告警渠道不能超过 %d 个", maxChannels)
	}

	names := make(map[string]bool, len(c.Channels))
	for _, channel := range c.Channels {
		if err := channel.validate(); err != nil {
			return fmt.Errorf("渠道「%s」%v", channel.Name, err)
		}
		if names[channel.Name] {
			return fmt.Errorf("渠道名称重复: %s", channel.Name)
		}
		names[channel.Name] = true
	}
	return nil
}

func (channel Channel) normalize() Channel {
	channel.ID = strings.TrimSpace(channel.ID)
	channel.Type = strings.ToLower(strings.TrimSpace(channel.Type))
	channel.Name = strings.TrimSpace(channel.Name)
	if channel.Name == "" {
		channel.Name = typeLabel(channel.Type)
	}

	channel.URL = strings.TrimSpace(channel.URL)
	channel.Secret = strings.TrimSpace(channel.Secret)
	channel.SMTPHost = strings.TrimSpace(channel.SMTPHost)
	channel.SMTPUsername = strings.TrimSpace(channel.SMTPUsername)
	channel.From = strings.TrimSpace(channel.From)

	switch channel.Type {
	case TypeEmail:
		// 邮箱渠道不使用 Webhook 字段。
		channel.URL = ""
		channel.Secret = ""
	case TypeDingTalk, TypeFeishu:
		// 钉钉/飞书使用 Webhook + 加签密钥。
		channel.SMTPHost = ""
		channel.SMTPPort = 0
		channel.SMTPUsername = ""
		channel.SMTPPassword = ""
		channel.From = ""
		channel.To = nil
		channel.UseTLS = false
	default:
		// 企业微信、Slack 与通用 Webhook 不需要加签密钥与 SMTP 字段。
		channel.Secret = ""
		channel.SMTPHost = ""
		channel.SMTPPort = 0
		channel.SMTPUsername = ""
		channel.SMTPPassword = ""
		channel.From = ""
		channel.To = nil
		channel.UseTLS = false
	}

	if channel.Type == TypeEmail && channel.SMTPPort == 0 {
		if channel.UseTLS {
			channel.SMTPPort = 465
		} else {
			channel.SMTPPort = 587
		}
	}

	if channel.Events == nil {
		channel.Events = []string{notify.EventFailed, notify.EventRecovered}
	}
	events := make([]string, 0, len(channel.Events))
	seen := make(map[string]bool, len(channel.Events))
	for _, event := range channel.Events {
		event = strings.ToLower(strings.TrimSpace(event))
		if event == "" || seen[event] {
			continue
		}
		seen[event] = true
		events = append(events, event)
	}
	channel.Events = events

	recipients := make([]string, 0, len(channel.To))
	for _, to := range channel.To {
		to = strings.TrimSpace(to)
		if to != "" {
			recipients = append(recipients, to)
		}
	}
	channel.To = recipients
	return channel
}

func (channel Channel) validate() error {
	if !allowedTypes[channel.Type] {
		return fmt.Errorf("不支持的渠道类型: %s", channel.Type)
	}
	if channel.Name == "" {
		return fmt.Errorf("名称不能为空")
	}
	if len([]rune(channel.Name)) > 100 {
		return fmt.Errorf("名称不能超过 100 个字符")
	}
	if len(channel.Events) == 0 {
		return fmt.Errorf("至少选择一个告警事件")
	}
	for _, event := range channel.Events {
		if !allowedEvents[event] {
			return fmt.Errorf("不支持的告警事件: %s", event)
		}
	}

	switch channel.Type {
	case TypeWebhook, TypeDingTalk, TypeFeishu, TypeWeCom, TypeSlack:
		if err := validateHTTPURL(channel.URL); err != nil {
			return err
		}
	case TypeEmail:
		if channel.SMTPHost == "" {
			return fmt.Errorf("SMTP 服务器不能为空")
		}
		if channel.SMTPPort < 1 || channel.SMTPPort > 65535 {
			return fmt.Errorf("SMTP 端口必须在 1 到 65535 之间")
		}
		from := channel.From
		if from == "" {
			from = channel.SMTPUsername
		}
		if from == "" {
			return fmt.Errorf("发件人不能为空")
		}
		if _, err := mail.ParseAddress(from); err != nil {
			return fmt.Errorf("发件人地址不正确: %v", err)
		}
		if len(channel.To) == 0 {
			return fmt.Errorf("至少填写一个收件人")
		}
		if len(channel.To) > maxRecipients {
			return fmt.Errorf("收件人不能超过 %d 个", maxRecipients)
		}
		for _, to := range channel.To {
			if _, err := mail.ParseAddress(to); err != nil {
				return fmt.Errorf("收件人地址不正确: %s", to)
			}
		}
	}
	return nil
}

func validateHTTPURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("Webhook 地址不能为空")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("Webhook 地址必须是 http 或 https URL")
	}
	return nil
}

func typeLabel(channelType string) string {
	if label, ok := typeLabels[channelType]; ok {
		return label
	}
	return "告警渠道"
}

func newChannelID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("ch_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func (c Config) clone() Config {
	out := c
	out.Channels = make([]Channel, len(c.Channels))
	for i, channel := range c.Channels {
		out.Channels[i] = channel
		out.Channels[i].Events = append([]string(nil), channel.Events...)
		out.Channels[i].To = append([]string(nil), channel.To...)
	}
	return out
}

// masked 返回给前端展示的配置副本，隐藏口令与签名密钥。
func (c Config) masked() Config {
	out := c.clone()
	for i := range out.Channels {
		if out.Channels[i].SMTPPassword != "" {
			out.Channels[i].SMTPPassword = MaskedSecret
		}
		if out.Channels[i].Secret != "" {
			out.Channels[i].Secret = MaskedSecret
		}
	}
	return out
}

// Manager 持有告警配置并负责发送与记录告警日志。
type Manager struct {
	store *store.Store

	mu  sync.RWMutex
	cfg Config

	pruneMu   sync.Mutex
	lastPrune time.Time
}

// New 创建告警管理器并从管理库加载配置；旧版通知设置会自动迁移为一条渠道。
func New(dataStore *store.Store) *Manager {
	m := &Manager{store: dataStore, cfg: DefaultConfig()}
	m.load()
	return m
}

func (m *Manager) load() {
	if m.store == nil {
		return
	}
	raw, found, err := m.store.GetSetting(SettingKey)
	if err != nil {
		log.Printf("读取告警配置失败: %v", err)
		return
	}
	if found && strings.TrimSpace(raw) != "" {
		var cfg Config
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			log.Printf("解析告警配置失败: %v", err)
			return
		}
		m.cfg = cfg.Normalize()
		return
	}
	if cfg, ok := m.migrateLegacy(); ok {
		m.cfg = cfg
	}
}

func (m *Manager) migrateLegacy() (Config, bool) {
	raw, found, err := m.store.GetSetting(legacySettingKey)
	if err != nil || !found || strings.TrimSpace(raw) == "" {
		return Config{}, false
	}

	var legacy struct {
		Enabled        bool     `json:"enabled"`
		Type           string   `json:"type"`
		URL            string   `json:"url"`
		Events         []string `json:"events"`
		TimeoutSeconds int      `json:"timeoutSeconds"`
	}
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		log.Printf("解析旧通知设置失败: %v", err)
		return Config{}, false
	}
	if strings.TrimSpace(legacy.URL) == "" {
		return Config{}, false
	}

	channelType := legacyChannelType(legacy.Type)
	events := make([]string, 0, len(legacy.Events))
	for _, event := range legacy.Events {
		event = strings.ToLower(strings.TrimSpace(event))
		if allowedEvents[event] {
			events = append(events, event)
		}
	}
	if len(events) == 0 {
		events = []string{notify.EventFailed, notify.EventRecovered}
	}

	cfg := DefaultConfig()
	cfg.Enabled = legacy.Enabled
	if legacy.TimeoutSeconds > 0 {
		cfg.TimeoutSeconds = legacy.TimeoutSeconds
	}
	cfg.Channels = []Channel{{
		Name:    "系统通知（迁移）",
		Type:    channelType,
		Enabled: true,
		Events:  events,
		URL:     legacy.URL,
	}}
	return cfg.Normalize(), true
}

func legacyChannelType(legacyType string) string {
	switch strings.ToLower(strings.TrimSpace(legacyType)) {
	case notify.TypeFeishu:
		return TypeFeishu
	case notify.TypeDingTalk:
		return TypeDingTalk
	case notify.TypeWeCom:
		return TypeWeCom
	case notify.TypeSlack:
		return TypeSlack
	default:
		return TypeWebhook
	}
}

// Config 返回当前配置的展示副本（隐藏密钥）。
func (m *Manager) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.masked()
}

func (m *Manager) snapshot() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.clone()
}

// Update 校验并持久化配置，然后立即生效。
func (m *Manager) Update(cfg Config) error {
	cfg = cfg.Normalize()
	cfg = mergeSecrets(cfg, m.snapshot())
	if err := cfg.Validate(); err != nil {
		return err
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("编码告警配置失败: %w", err)
	}
	if m.store != nil {
		if err := m.store.SaveSetting(SettingKey, string(data)); err != nil {
			return fmt.Errorf("保存告警配置失败: %w", err)
		}
	}

	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	return nil
}

// mergeSecrets 保留前端回传的掩码占位符所对应的真实密钥。
func mergeSecrets(cfg Config, current Config) Config {
	existing := make(map[string]Channel, len(current.Channels))
	for _, channel := range current.Channels {
		existing[channel.ID] = channel
	}

	for i := range cfg.Channels {
		channel := &cfg.Channels[i]
		previous, ok := existing[channel.ID]
		if !ok || previous.Type != channel.Type {
			if channel.SMTPPassword == MaskedSecret {
				channel.SMTPPassword = ""
			}
			if channel.Secret == MaskedSecret {
				channel.Secret = ""
			}
			continue
		}
		if channel.Type == TypeEmail &&
			(channel.SMTPPassword == "" || channel.SMTPPassword == MaskedSecret) {
			channel.SMTPPassword = previous.SMTPPassword
		}
		if (channel.Type == TypeDingTalk || channel.Type == TypeFeishu) &&
			(channel.Secret == "" || channel.Secret == MaskedSecret) {
			channel.Secret = previous.Secret
		}
	}
	return cfg
}

// Test 使用给定渠道配置发送一条测试消息，并写入告警日志。
func (m *Manager) Test(channel Channel, timeoutSeconds int) error {
	cfg := m.snapshot()
	channel = mergeSecrets(Config{Channels: []Channel{channel}}, cfg).Channels[0]
	channel = channel.normalize()
	if err := channel.validate(); err != nil {
		return err
	}
	if timeoutSeconds <= 0 || timeoutSeconds > 60 {
		timeoutSeconds = cfg.TimeoutSeconds
		if timeoutSeconds <= 0 {
			timeoutSeconds = defaultTimeoutSeconds
		}
	}

	started := time.Now()
	err := send(channel, notify.EventTest, store.JobRun{}, time.Duration(timeoutSeconds)*time.Second)
	m.record(channel, notify.EventTest, store.JobRun{}, started, err, cfg.RetentionDays)
	return err
}

// Notify 在运行结束时按事件把告警发送到所有匹配的渠道。
func (m *Manager) Notify(run store.JobRun, willRetry bool) {
	cfg := m.snapshot()
	if !cfg.Enabled {
		return
	}

	event := notify.EventFor(run, willRetry)
	if event == "" {
		return
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	for _, channel := range cfg.Channels {
		if !channel.Enabled || !channelHasEvent(channel, event) {
			continue
		}
		go func(channel Channel) {
			started := time.Now()
			err := send(channel, event, run, timeout)
			m.record(channel, event, run, started, err, cfg.RetentionDays)
		}(channel)
	}
}

func channelHasEvent(channel Channel, event string) bool {
	for _, candidate := range channel.Events {
		if candidate == event {
			return true
		}
	}
	return false
}

func (m *Manager) record(
	channel Channel,
	event string,
	run store.JobRun,
	started time.Time,
	sendErr error,
	retentionDays int,
) {
	if sendErr != nil {
		log.Printf("发送告警失败（渠道 %s，事件 %s）: %v", channel.Name, event, sendErr)
	}
	if m.store == nil {
		return
	}

	entry := store.AlertLog{
		Event:           event,
		Status:          store.AlertStatusSuccess,
		ChannelID:       channel.ID,
		ChannelName:     channel.Name,
		ChannelType:     channel.Type,
		RunID:           run.ID,
		SyncJobID:       run.SyncJobID,
		SyncJobName:     run.SyncJobName,
		CollectTaskID:   run.CollectTaskID,
		CollectTaskName: run.CollectTaskName,
		DurationMS:      time.Since(started).Milliseconds(),
		CreatedAt:       time.Now(),
	}
	if sendErr != nil {
		entry.Status = store.AlertStatusFailed
		entry.Error = notify.TruncateRunes(strings.ReplaceAll(sendErr.Error(), "\n", " "), 900)
	}
	if err := m.store.CreateAlertLog(&entry); err != nil {
		log.Printf("写入告警日志失败: %v", err)
		return
	}
	m.prune(retentionDays)
}

// prune 按保留天数清理历史告警日志，最多每小时执行一次。
func (m *Manager) prune(retentionDays int) {
	if retentionDays <= 0 {
		return
	}

	m.pruneMu.Lock()
	defer m.pruneMu.Unlock()
	if time.Since(m.lastPrune) < time.Hour {
		return
	}
	m.lastPrune = time.Now()

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	if _, err := m.store.DeleteAlertLogs(&cutoff); err != nil {
		log.Printf("清理告警日志失败: %v", err)
	}
}
