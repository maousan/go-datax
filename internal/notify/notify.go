// Package notify 提供告警消息的构造与 HTTP 投递原语。
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

// 事件名称。
const (
	EventFailed    = "failed"
	EventRecovered = "recovered"
	EventSucceeded = "succeeded"
	EventCanceled  = "canceled"
	EventTest      = "test"
)

// 消息类型。
const (
	TypeGeneric  = "generic"
	TypeFeishu   = "feishu"
	TypeDingTalk = "dingtalk"
	TypeWeCom    = "wecom"
	TypeSlack    = "slack"
)

// EventFor 返回运行结果对应的事件名；空字符串表示不需要发送。
// willRetry 为 true 表示失败后还会自动重试，此时不提前告警。
func EventFor(run store.JobRun, willRetry bool) string {
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

// EventTitle 返回事件的中文标题。
func EventTitle(event string) string {
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
		return "告警测试"
	default:
		return event
	}
}

type genericPayload struct {
	SourceKind      string  `json:"sourceKind"`
	Event           string  `json:"event"`
	Status          string  `json:"status"`
	JobID           int64   `json:"jobId"`
	JobName         string  `json:"jobName"`
	CollectTaskID   *int64  `json:"collectTaskId,omitempty"`
	CollectTaskName string  `json:"collectTaskName,omitempty"`
	RunID           int64   `json:"runId"`
	Trigger         string  `json:"trigger"`
	RetryAttempt    int     `json:"retryAttempt"`
	StartedAt       string  `json:"startedAt"`
	FinishedAt      string  `json:"finishedAt,omitempty"`
	DurationSeconds float64 `json:"durationSeconds"`
	ExitCode        *int    `json:"exitCode,omitempty"`
	ErrorSummary    string  `json:"errorSummary,omitempty"`
}

// RunPayload 构造通用 Webhook 的结构化运行事件。
func RunPayload(event string, run store.JobRun) ([]byte, error) {
	return json.Marshal(genericPayload{
		SourceKind:      store.AlertSourceDataX,
		Event:           event,
		Status:          run.Status,
		JobID:           jobID(run),
		JobName:         run.SyncJobName,
		CollectTaskID:   run.CollectTaskID,
		CollectTaskName: run.CollectTaskName,
		RunID:           run.ID,
		Trigger:         run.Trigger,
		RetryAttempt:    run.RetryAttempt,
		StartedAt:       run.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt:      finishedAt(run),
		DurationSeconds: durationSeconds(run),
		ExitCode:        run.ExitCode,
		ErrorSummary:    TruncateRunes(strings.ReplaceAll(run.ErrorSummary, "\n", " "), 500),
	})
}

// TextPayload 构造 IM 文本消息体。
// PipelineEventFor returns the alert event for a Pipeline run.
func PipelineEventFor(run store.PipelineRun, willRetry bool) string {
	switch run.Status {
	case store.PipelineRunStatusFailed:
		if willRetry {
			return ""
		}
		return EventFailed
	case store.PipelineRunStatusCanceled:
		return EventCanceled
	case store.PipelineRunStatusSucceeded:
		if run.Trigger == store.PipelineRunTriggerRetry {
			return EventRecovered
		}
		return EventSucceeded
	default:
		return ""
	}
}

type pipelinePayload struct {
	SourceKind      string  `json:"sourceKind"`
	Event           string  `json:"event"`
	Status          string  `json:"status"`
	PipelineID      int64   `json:"pipelineId"`
	PipelineName    string  `json:"pipelineName"`
	VersionID       *int64  `json:"versionId,omitempty"`
	VersionNumber   int     `json:"versionNumber"`
	RunID           int64   `json:"runId"`
	ScheduleID      *int64  `json:"scheduleId,omitempty"`
	Trigger         string  `json:"trigger"`
	RetryAttempt    int     `json:"retryAttempt"`
	StartedAt       string  `json:"startedAt"`
	FinishedAt      string  `json:"finishedAt,omitempty"`
	DurationSeconds float64 `json:"durationSeconds"`
	ReadRecords     int64   `json:"readRecords"`
	WriteRecords    int64   `json:"writeRecords"`
	ErrorSummary    string  `json:"errorSummary,omitempty"`
}

// PipelineRunPayload builds the structured generic-webhook payload for a Pipeline run.
func PipelineRunPayload(event string, run store.PipelineRun) ([]byte, error) {
	return json.Marshal(pipelinePayload{
		SourceKind:      store.AlertSourcePipeline,
		Event:           event,
		Status:          run.Status,
		PipelineID:      pipelineID(run),
		PipelineName:    run.PipelineName,
		VersionID:       run.VersionID,
		VersionNumber:   run.VersionNumber,
		RunID:           run.ID,
		ScheduleID:      run.ScheduleID,
		Trigger:         run.Trigger,
		RetryAttempt:    run.RetryAttempt,
		StartedAt:       run.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt:      pipelineFinishedAt(run),
		DurationSeconds: pipelineDurationSeconds(run),
		ReadRecords:     run.ReadRecords,
		WriteRecords:    run.WriteRecords,
		ErrorSummary:    TruncateRunes(strings.ReplaceAll(run.ErrorSummary, "\n", " "), 500),
	})
}

// FormatPipelineRunText formats a Pipeline alert for text-based channels.
func FormatPipelineRunText(event string, run store.PipelineRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【go-datax】%s\n", EventTitle(event))
	fmt.Fprintf(&b, "Pipeline：%s\n", run.PipelineName)
	fmt.Fprintf(&b, "版本：v%d\n", run.VersionNumber)
	trigger := triggerLabel(run.Trigger)
	if run.RetryAttempt > 0 {
		trigger = fmt.Sprintf("%s #%d", trigger, run.RetryAttempt)
	}
	fmt.Fprintf(&b, "运行：#%d · %s\n", run.ID, trigger)
	if run.ScheduleID != nil {
		fmt.Fprintf(&b, "调度：#%d\n", *run.ScheduleID)
	}
	fmt.Fprintf(&b, "开始：%s\n", formatLocal(run.StartedAt))
	if run.FinishedAt != nil {
		fmt.Fprintf(&b, "结束：%s\n", formatLocal(*run.FinishedAt))
		fmt.Fprintf(&b, "耗时：%s\n", formatDuration(*run.FinishedAt, run.StartedAt))
	}
	fmt.Fprintf(&b, "读取/写入：%d / %d 行\n", run.ReadRecords, run.WriteRecords)
	if summary := TruncateRunes(strings.ReplaceAll(run.ErrorSummary, "\n", " "), 300); summary != "" {
		fmt.Fprintf(&b, "错误：%s\n", summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

func TextPayload(eventType, text string) ([]byte, error) {
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
		return nil, fmt.Errorf("不支持的消息类型: %s", eventType)
	}
}

// FormatRunText 把运行事件格式化为可读文本。
func FormatRunText(event string, run store.JobRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【go-datax】%s\n", EventTitle(event))
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
	if summary := TruncateRunes(strings.ReplaceAll(run.ErrorSummary, "\n", " "), 300); summary != "" {
		fmt.Fprintf(&b, "错误：%s\n", summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Post 把 JSON 消息体发送到 Webhook 地址。
func Post(rawURL string, timeout time.Duration, body []byte) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(body))
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
		message := TruncateRunes(strings.TrimSpace(string(data)), 300)
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

var triggerLabels = map[string]string{
	"manual":   "手动触发",
	"rerun":    "重跑",
	"retry":    "自动重试",
	"schedule": "定时调度",
}

func triggerLabel(trigger string) string {
	if label, ok := triggerLabels[trigger]; ok {
		return label
	}
	return trigger
}

func pipelineID(run store.PipelineRun) int64 {
	if run.PipelineID == nil {
		return 0
	}
	return *run.PipelineID
}

func pipelineFinishedAt(run store.PipelineRun) string {
	if run.FinishedAt == nil {
		return ""
	}
	return run.FinishedAt.UTC().Format(time.RFC3339)
}

func pipelineDurationSeconds(run store.PipelineRun) float64 {
	if run.FinishedAt == nil {
		return 0
	}
	seconds := run.FinishedAt.Sub(run.StartedAt).Seconds()
	if seconds < 0 {
		return 0
	}
	return seconds
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

// TruncateRunes 按字符截断文本，超长时追加省略号。
func TruncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
