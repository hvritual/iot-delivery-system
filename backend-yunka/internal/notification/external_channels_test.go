package notification_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
)

func TestWebhookChannelPostsSignedIdempotentNotification(t *testing.T) {
	t.Parallel()

	secret := "local-webhook-test-secret"
	received := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received <- request.Clone(request.Context())
		writer.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	channel, err := notification.NewWebhookChannel(notification.WebhookConfig{
		Name:     "release-webhook",
		Endpoint: server.URL,
		Secret:   secret,
	})
	if err != nil {
		t.Fatalf("create webhook channel: %v", err)
	}
	note := notification.Notification{DeliveryID: "event-001", EventType: "delivery.work-item.created", Subject: "IOT-001", Title: "新建事项", OccurredAt: time.Now().UTC()}
	if err := channel.Deliver(context.Background(), note); err != nil {
		t.Fatalf("deliver webhook notification: %v", err)
	}
	request := <-received
	if request.Method != http.MethodPost || request.Header.Get("Idempotency-Key") != note.DeliveryID {
		t.Fatalf("webhook request = %#v, want POST with stable idempotency key", request)
	}
	body, err := json.Marshal(note)
	if err != nil {
		t.Fatalf("encode expected payload: %v", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if request.Header.Get("X-IoT-Delivery-Signature") != wantSignature {
		t.Fatalf("webhook signature = %q, want %q", request.Header.Get("X-IoT-Delivery-Signature"), wantSignature)
	}
}

func TestExternalNotificationChannelsEncodeWeComAndSMTPWithoutNetworkSideEffects(t *testing.T) {
	t.Parallel()

	wecomBody := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode WeCom body: %v", err)
		}
		wecomBody <- body
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	wecom, err := notification.NewWeComRobotChannel(notification.WeComRobotConfig{Endpoint: server.URL})
	if err != nil {
		t.Fatalf("create WeCom channel: %v", err)
	}
	note := notification.Notification{DeliveryID: "event-002", EventType: "delivery.work-item.closed", Subject: "IOT-002", Title: "事项关闭", Body: "复盘已归档", OccurredAt: time.Now().UTC()}
	if err := wecom.Deliver(context.Background(), note); err != nil {
		t.Fatalf("deliver WeCom notification: %v", err)
	}
	payload := <-wecomBody
	if payload["msgtype"] != "markdown" || !strings.Contains(payload["markdown"].(map[string]any)["content"].(string), note.Title) {
		t.Fatalf("WeCom payload = %#v, want markdown containing the notification title", payload)
	}

	sender := &recordingSMTPSender{}
	email, err := notification.NewSMTPChannel(notification.SMTPConfig{
		Address: "smtp.example.test:587",
		From:    "delivery@example.test",
		To:      []string{"team@example.test"},
	}, sender)
	if err != nil {
		t.Fatalf("create SMTP channel: %v", err)
	}
	if err := email.Deliver(context.Background(), note); err != nil {
		t.Fatalf("deliver SMTP notification: %v", err)
	}
	if sender.message.From != "delivery@example.test" || len(sender.message.To) != 1 || sender.message.To[0] != "team@example.test" || !strings.Contains(string(sender.message.Data), "Subject: 事项关闭") {
		t.Fatalf("SMTP message = %#v, want configured recipient and title", sender.message)
	}
}

func TestChannelsFromEnvironmentOnlyEnablesExplicitCompleteConfigurations(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"IOT_DELIVERY_NOTIFICATION_WEBHOOK_URL":       "https://hooks.example.test/delivery",
		"IOT_DELIVERY_NOTIFICATION_WEBHOOK_SECRET":    "signature-secret",
		"IOT_DELIVERY_NOTIFICATION_WECOM_WEBHOOK_URL": "https://wecom.example.test/robot",
		"IOT_DELIVERY_NOTIFICATION_SMTP_ADDRESS":      "smtp.example.test:587",
		"IOT_DELIVERY_NOTIFICATION_SMTP_FROM":         "delivery@example.test",
		"IOT_DELIVERY_NOTIFICATION_SMTP_TO":           "team@example.test, release@example.test",
	}
	channels, err := notification.ChannelsFromEnvironment(func(name string) string { return values[name] })
	if err != nil {
		t.Fatalf("configure channels from environment: %v", err)
	}
	names := make([]string, 0, len(channels))
	for _, channel := range channels {
		names = append(names, channel.Name())
	}
	if strings.Join(names, ",") != "webhook,wecom-robot,email" {
		t.Fatalf("configured notification channels = %v, want webhook,wecom-robot,email", names)
	}
	if _, err := notification.ChannelsFromEnvironment(func(name string) string {
		if name == "IOT_DELIVERY_NOTIFICATION_SMTP_TO" {
			return "team@example.test"
		}
		return ""
	}); err == nil {
		t.Fatal("partial SMTP configuration should fail instead of silently enabling a misconfigured external channel")
	}
}

type recordingSMTPSender struct {
	message notification.SMTPMessage
}

func (sender *recordingSMTPSender) Send(_ context.Context, message notification.SMTPMessage) error {
	sender.message = message
	return nil
}
