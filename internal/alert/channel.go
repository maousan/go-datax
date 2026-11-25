package alert

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/notify"
	"github.com/longkeyy/go-datax/internal/store"
)

// send 按渠道类型投递一条告警。
func send(channel Channel, event string, run store.JobRun, timeout time.Duration) error {
	switch channel.Type {
	case TypeEmail:
		return sendEmail(channel, event, run, timeout)
	case TypeWebhook:
		if event == notify.EventTest {
			body, err := json.Marshal(map[string]any{
				"sourceKind": store.AlertSourceDataX,
				"event":      event,
				"message":    "go-datax 告警测试",
			})
			if err != nil {
				return err
			}
			return notify.Post(channel.URL, timeout, body)
		}
		body, err := notify.RunPayload(event, run)
		if err != nil {
			return err
		}
		return notify.Post(channel.URL, timeout, body)
	default:
		return dispatchIM(channel, eventText(event, run), timeout)
	}
}

// sendPipeline dispatches a Pipeline alert through one channel.
func sendPipeline(channel Channel, event string, run store.PipelineRun, timeout time.Duration) error {
	text := notify.FormatPipelineRunText(event, run)
	switch channel.Type {
	case TypeEmail:
		return sendEmailText(channel, event, run.PipelineName, text, timeout)
	case TypeWebhook:
		body, err := notify.PipelineRunPayload(event, run)
		if err != nil {
			return err
		}
		return notify.Post(channel.URL, timeout, body)
	default:
		return dispatchIM(channel, text, timeout)
	}
}

func eventText(event string, run store.JobRun) string {
	if event == notify.EventTest {
		return "【go-datax】告警测试\n渠道配置可用。"
	}
	return notify.FormatRunText(event, run)
}

// signDingTalkURL 按钉钉机器人要求追加 timestamp/sign 查询参数。

func dispatchIM(channel Channel, text string, timeout time.Duration) error {
	switch channel.Type {
	case TypeDingTalk:
		target, err := signDingTalkURL(channel.URL, channel.Secret)
		if err != nil {
			return err
		}
		body, err := notify.TextPayload(notify.TypeDingTalk, text)
		if err != nil {
			return err
		}
		return notify.Post(target, timeout, body)
	case TypeFeishu:
		body, err := feishuPayload(channel.Secret, text)
		if err != nil {
			return err
		}
		return notify.Post(channel.URL, timeout, body)
	case TypeWeCom:
		body, err := notify.TextPayload(notify.TypeWeCom, text)
		if err != nil {
			return err
		}
		return notify.Post(channel.URL, timeout, body)
	case TypeSlack:
		body, err := notify.TextPayload(notify.TypeSlack, text)
		if err != nil {
			return err
		}
		return notify.Post(channel.URL, timeout, body)
	default:
		return fmt.Errorf("不支持的告警渠道类型: %s", channel.Type)
	}
}

func signDingTalkURL(rawURL, secret string) (string, error) {
	if secret == "" {
		return rawURL, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("解析钉钉 Webhook 地址失败: %w", err)
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	query := parsed.Query()
	query.Set("timestamp", timestamp)
	query.Set("sign", hmacBase64([]byte(secret), []byte(timestamp+"\n"+secret)))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// feishuPayload 构造飞书文本消息，配置密钥时附带签名。
func feishuPayload(secret, text string) ([]byte, error) {
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	}
	if secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		payload["timestamp"] = timestamp
		// 飞书签名把 timestamp+secret 作为 HMAC 密钥，消息体为空。
		payload["sign"] = hmacBase64([]byte(timestamp+"\n"+secret), nil)
	}
	return json.Marshal(payload)
}

func hmacBase64(key, message []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func sendEmail(channel Channel, event string, run store.JobRun, timeout time.Duration) error {
	return sendEmailText(channel, event, run.SyncJobName, eventText(event, run), timeout)
}

func sendEmailText(channel Channel, event, subjectName, text string, timeout time.Duration) error {
	from, err := mail.ParseAddress(channel.From)
	if err != nil {
		// 未填写发件人时沿用 SMTP 用户名。
		from, err = mail.ParseAddress(channel.SMTPUsername)
		if err != nil {
			return fmt.Errorf("发件人地址不正确: %v", err)
		}
	}

	recipients := make([]string, 0, len(channel.To))
	for _, raw := range channel.To {
		address, err := mail.ParseAddress(raw)
		if err != nil {
			return fmt.Errorf("收件人地址不正确: %s", raw)
		}
		recipients = append(recipients, address.Address)
	}

	subject := "【go-datax】告警测试"
	if event != notify.EventTest {
		subject = fmt.Sprintf("【go-datax】%s - %s", notify.EventTitle(event), subjectName)
	}
	return smtpSend(channel, from.Address, recipients, subject, text, timeout)
}

func smtpSend(
	channel Channel,
	from string,
	recipients []string,
	subject string,
	text string,
	timeout time.Duration,
) error {
	address := net.JoinHostPort(channel.SMTPHost, strconv.Itoa(channel.SMTPPort))
	tlsConfig := &tls.Config{
		ServerName: channel.SMTPHost,
		MinVersion: tls.VersionTLS12,
	}
	dialer := &net.Dialer{Timeout: timeout}

	var conn net.Conn
	var err error
	if channel.UseTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	client, err := smtp.NewClient(conn, channel.SMTPHost)
	if err != nil {
		return fmt.Errorf("创建 SMTP 会话失败: %w", err)
	}
	defer client.Close()

	if !channel.UseTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("SMTP STARTTLS 失败: %w", err)
			}
		}
	}

	if channel.SMTPUsername != "" {
		auth := smtp.PlainAuth("", channel.SMTPUsername, channel.SMTPPassword, channel.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("设置收件人 %s 失败: %w", recipient, err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("准备邮件内容失败: %w", err)
	}
	if _, err := writer.Write(buildMail(from, recipients, subject, text)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("发送邮件内容失败: %w", err)
	}
	return client.Quit()
}

func buildMail(from string, recipients []string, subject, text string) []byte {
	var message strings.Builder
	message.WriteString("From: go-datax <" + from + ">\r\n")
	message.WriteString("To: " + strings.Join(recipients, ", ") + "\r\n")
	message.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	message.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	message.WriteString("MIME-Version: 1.0\r\n")
	message.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	message.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")

	encoded := base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(text, "\n", "\r\n")))
	for len(encoded) > 76 {
		message.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	message.WriteString(encoded + "\r\n")
	return []byte(message.String())
}
