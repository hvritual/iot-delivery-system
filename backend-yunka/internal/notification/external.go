package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

const (
	defaultWebhookChannelName = "webhook"
	defaultWeComChannelName   = "wecom-robot"
	defaultSMTPChannelName    = "email"
)

// WebhookConfig defines a signed, JSON notification endpoint. The endpoint is
// intentionally supplied by runtime configuration; no external destination is
// embedded in application code.
type WebhookConfig struct {
	Name     string
	Endpoint string
	Secret   string
	Client   *http.Client
}

// WebhookChannel posts the channel-neutral Notification envelope to a generic
// HTTP endpoint. The delivery ID is sent as both an idempotency key and a
// signed JSON payload so receivers can safely handle outbox retries.
type WebhookChannel struct {
	name     string
	endpoint string
	secret   string
	client   *http.Client
}

func NewWebhookChannel(configuration WebhookConfig) (*WebhookChannel, error) {
	name := strings.TrimSpace(configuration.Name)
	if name == "" {
		name = defaultWebhookChannelName
	}
	endpoint, err := normalizeHTTPEndpoint(configuration.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("configure notification webhook: %w", err)
	}
	client := configuration.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &WebhookChannel{name: name, endpoint: endpoint, secret: strings.TrimSpace(configuration.Secret), client: client}, nil
}

func (channel *WebhookChannel) Name() string {
	if channel == nil {
		return ""
	}
	return channel.name
}

func (channel *WebhookChannel) Deliver(ctx context.Context, value Notification) error {
	if channel == nil || channel.client == nil {
		return errors.New("notification webhook channel is not configured")
	}
	value, err := normalize(value)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode notification webhook payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create notification webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "iot-delivery-system/notification")
	request.Header.Set("Idempotency-Key", value.DeliveryID)
	request.Header.Set("X-IoT-Delivery-Event", value.EventType)
	if channel.secret != "" {
		request.Header.Set("X-IoT-Delivery-Signature", hmacSHA256(channel.secret, payload))
	}
	return executeHTTPNotification(ctx, channel.client, request, "notification webhook")
}

// WeComRobotConfig holds the robot webhook URL issued by Enterprise WeChat.
// It is a dedicated adapter because its expected body differs from a generic
// webhook, while the delivery and retry semantics remain identical.
type WeComRobotConfig struct {
	Name     string
	Endpoint string
	Client   *http.Client
}

type WeComRobotChannel struct {
	name     string
	endpoint string
	client   *http.Client
}

func NewWeComRobotChannel(configuration WeComRobotConfig) (*WeComRobotChannel, error) {
	name := strings.TrimSpace(configuration.Name)
	if name == "" {
		name = defaultWeComChannelName
	}
	endpoint, err := normalizeHTTPEndpoint(configuration.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("configure WeCom robot channel: %w", err)
	}
	client := configuration.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &WeComRobotChannel{name: name, endpoint: endpoint, client: client}, nil
}

func (channel *WeComRobotChannel) Name() string {
	if channel == nil {
		return ""
	}
	return channel.name
}

func (channel *WeComRobotChannel) Deliver(ctx context.Context, value Notification) error {
	if channel == nil || channel.client == nil {
		return errors.New("WeCom robot channel is not configured")
	}
	value, err := normalize(value)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": formatWeComMarkdown(value)},
	})
	if err != nil {
		return fmt.Errorf("encode WeCom notification payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create WeCom notification request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", value.DeliveryID)
	request.Header.Set("X-IoT-Delivery-Event", value.EventType)
	return executeHTTPNotification(ctx, channel.client, request, "WeCom robot")
}

// SMTPConfig and SMTPMessage isolate the standard-library transport behind a
// small seam. Tests and alternate mail providers can supply an SMTPSender
// without opening a network connection.
type SMTPConfig struct {
	Name     string
	Address  string
	From     string
	To       []string
	Username string
	Password string
}

type SMTPMessage struct {
	Address  string
	From     string
	To       []string
	Username string
	Password string
	Data     []byte
}

type SMTPSender interface {
	Send(context.Context, SMTPMessage) error
}

type SMTPChannel struct {
	name   string
	config SMTPConfig
	sender SMTPSender
}

func NewSMTPChannel(configuration SMTPConfig, senders ...SMTPSender) (*SMTPChannel, error) {
	configuration.Name = strings.TrimSpace(configuration.Name)
	if configuration.Name == "" {
		configuration.Name = defaultSMTPChannelName
	}
	configuration.Address = strings.TrimSpace(configuration.Address)
	configuration.From = strings.TrimSpace(configuration.From)
	configuration.Username = strings.TrimSpace(configuration.Username)
	configuration.Password = strings.TrimSpace(configuration.Password)
	if configuration.Address == "" || configuration.From == "" || len(configuration.To) == 0 {
		return nil, errors.New("SMTP address, from, and at least one recipient are required")
	}
	if _, _, err := net.SplitHostPort(configuration.Address); err != nil {
		return nil, fmt.Errorf("invalid SMTP address: %w", err)
	}
	if _, err := mail.ParseAddress(configuration.From); err != nil {
		return nil, fmt.Errorf("invalid SMTP from address: %w", err)
	}
	to := make([]string, 0, len(configuration.To))
	for _, rawAddress := range configuration.To {
		address := strings.TrimSpace(rawAddress)
		if address == "" {
			continue
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil {
			return nil, fmt.Errorf("invalid SMTP recipient: %w", err)
		}
		to = append(to, parsed.Address)
	}
	if len(to) == 0 {
		return nil, errors.New("SMTP requires at least one non-empty recipient")
	}
	if (configuration.Username == "") != (configuration.Password == "") {
		return nil, errors.New("SMTP username and password must be configured together")
	}
	configuration.To = to
	var sender SMTPSender = smtpSender{}
	if len(senders) > 0 && senders[0] != nil {
		sender = senders[0]
	}
	return &SMTPChannel{name: configuration.Name, config: configuration, sender: sender}, nil
}

func (channel *SMTPChannel) Name() string {
	if channel == nil {
		return ""
	}
	return channel.name
}

func (channel *SMTPChannel) Deliver(ctx context.Context, value Notification) error {
	if channel == nil || channel.sender == nil {
		return errors.New("SMTP notification channel is not configured")
	}
	value, err := normalize(value)
	if err != nil {
		return err
	}
	subject := value.Title
	if subject == "" {
		subject = "IoT 交付通知"
	}
	message := SMTPMessage{
		Address:  channel.config.Address,
		From:     channel.config.From,
		To:       append([]string(nil), channel.config.To...),
		Username: channel.config.Username,
		Password: channel.config.Password,
		Data: []byte(strings.Join([]string{
			"From: " + channel.config.From,
			"To: " + strings.Join(channel.config.To, ", "),
			"Subject: " + subject,
			"MIME-Version: 1.0",
			"Content-Type: text/plain; charset=UTF-8",
			"X-IoT-Delivery-Event: " + value.EventType,
			"Idempotency-Key: " + value.DeliveryID,
			"",
			value.Body,
		}, "\r\n")),
	}
	if err := channel.sender.Send(ctx, message); err != nil {
		return fmt.Errorf("deliver SMTP notification: %w", err)
	}
	return nil
}

type smtpSender struct{}

func (smtpSender) Send(ctx context.Context, message SMTPMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(message.Address)
	if err != nil {
		return err
	}
	var auth smtp.Auth
	if message.Username != "" {
		auth = smtp.PlainAuth("", message.Username, message.Password, host)
	}
	return smtp.SendMail(message.Address, auth, message.From, message.To, message.Data)
}

// ChannelsFromEnvironment returns only channels whose complete configuration
// is explicitly present. An incomplete configuration is an error because
// silently enabling a partial external transport would hide a delivery gap.
func ChannelsFromEnvironment(getenv func(string) string) ([]Channel, error) {
	if getenv == nil {
		return nil, errors.New("notification environment reader is required")
	}
	value := func(name string) string { return strings.TrimSpace(getenv(name)) }
	channels := make([]Channel, 0, 3)
	webhookURL := value("IOT_DELIVERY_NOTIFICATION_WEBHOOK_URL")
	webhookSecret := value("IOT_DELIVERY_NOTIFICATION_WEBHOOK_SECRET")
	if webhookURL != "" || webhookSecret != "" {
		if webhookURL == "" {
			return nil, errors.New("IOT_DELIVERY_NOTIFICATION_WEBHOOK_URL is required when configuring a webhook notification")
		}
		channel, err := NewWebhookChannel(WebhookConfig{Name: value("IOT_DELIVERY_NOTIFICATION_WEBHOOK_NAME"), Endpoint: webhookURL, Secret: webhookSecret})
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	wecomURL := value("IOT_DELIVERY_NOTIFICATION_WECOM_WEBHOOK_URL")
	if wecomURL != "" {
		channel, err := NewWeComRobotChannel(WeComRobotConfig{Name: value("IOT_DELIVERY_NOTIFICATION_WECOM_NAME"), Endpoint: wecomURL})
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	smtpAddress := value("IOT_DELIVERY_NOTIFICATION_SMTP_ADDRESS")
	smtpFrom := value("IOT_DELIVERY_NOTIFICATION_SMTP_FROM")
	smtpTo := value("IOT_DELIVERY_NOTIFICATION_SMTP_TO")
	smtpUsername := value("IOT_DELIVERY_NOTIFICATION_SMTP_USERNAME")
	smtpPassword := value("IOT_DELIVERY_NOTIFICATION_SMTP_PASSWORD")
	if smtpAddress != "" || smtpFrom != "" || smtpTo != "" || smtpUsername != "" || smtpPassword != "" {
		channel, err := NewSMTPChannel(SMTPConfig{
			Name:     value("IOT_DELIVERY_NOTIFICATION_SMTP_NAME"),
			Address:  smtpAddress,
			From:     smtpFrom,
			To:       strings.Split(smtpTo, ","),
			Username: smtpUsername,
			Password: smtpPassword,
		})
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	return channels, nil
}

func normalizeHTTPEndpoint(value string) (string, error) {
	endpoint := strings.TrimSpace(value)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("endpoint must be an absolute http or https URL")
	}
	return endpoint, nil
}

func executeHTTPNotification(ctx context.Context, client *http.Client, request *http.Request, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("deliver %s: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
	message := strings.TrimSpace(string(body))
	if message == "" {
		return fmt.Errorf("deliver %s: unexpected HTTP status %d", label, response.StatusCode)
	}
	return fmt.Errorf("deliver %s: unexpected HTTP status %d: %s", label, response.StatusCode, message)
}

func hmacSHA256(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func formatWeComMarkdown(value Notification) string {
	title := value.Title
	if title == "" {
		title = "IoT 交付通知"
	}
	parts := []string{"**" + title + "**"}
	if value.Subject != "" {
		parts = append(parts, "> 事项："+value.Subject)
	}
	if value.Body != "" {
		parts = append(parts, value.Body)
	}
	return strings.Join(parts, "\n")
}
