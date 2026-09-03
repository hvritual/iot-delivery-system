package main

import (
	"strings"
	"testing"
	"time"
)

func TestConfigurationDefaultsToAnIsolatedYunkaMVP(t *testing.T) {
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "development")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "")
	t.Setenv("IOT_DELIVERY_YUNKA_HTTP_ADDR", "")
	t.Setenv("IOT_DELIVERY_YUNKA_GRPC_ADDR", "")
	t.Setenv("IOT_DELIVERY_YUNKA_DB", "")
	t.Setenv("IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT", "")
	t.Setenv("IOT_DELIVERY_DUE_REMINDER_LEAD_DAYS", "")
	t.Setenv("IOT_DELIVERY_DUE_REMINDER_INTERVAL", "")

	configuration, err := configurationFromEnv()
	if err != nil {
		t.Fatalf("read default configuration: %v", err)
	}
	if configuration.HTTPAddress != "127.0.0.1:8281" {
		t.Fatalf("default HTTP address = %q", configuration.HTTPAddress)
	}
	if configuration.GRPCAddress != "127.0.0.1:8282" {
		t.Fatalf("default gRPC address = %q", configuration.GRPCAddress)
	}
	if configuration.DatabasePath != "data/iot-delivery-yunka.db" {
		t.Fatalf("default database path = %q", configuration.DatabasePath)
	}
	if configuration.ObsidianVault != "runtime-vault" {
		t.Fatalf("default Obsidian vault = %q", configuration.ObsidianVault)
	}
	if len(configuration.NotificationChannels) != 0 {
		t.Fatalf("default notification channels = %#v, want only the internal local inbox", configuration.NotificationChannels)
	}
	if configuration.DueReminder.LeadDays != 1 || configuration.DueReminder.Interval != time.Hour {
		t.Fatalf("default due reminder config = %#v, want one-day lead and hourly schedule", configuration.DueReminder)
	}
	if configuration.RuntimeEnvironment != "development" || configuration.BootstrapMode != "disabled" {
		t.Fatalf("default startup policy = environment=%q bootstrap=%q, want development with disabled bootstrap", configuration.RuntimeEnvironment, configuration.BootstrapMode)
	}
}

func TestConfigurationReadsDueReminderWindowFromEnvironment(t *testing.T) {
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "development")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv("IOT_DELIVERY_DUE_REMINDER_LEAD_DAYS", "3")
	t.Setenv("IOT_DELIVERY_DUE_REMINDER_INTERVAL", "30m")

	configuration, err := configurationFromEnv()
	if err != nil {
		t.Fatalf("read due reminder configuration: %v", err)
	}
	if configuration.DueReminder.LeadDays != 3 || configuration.DueReminder.Interval != 30*time.Minute {
		t.Fatalf("due reminder config = %#v, want configured window", configuration.DueReminder)
	}
}

func TestConfigurationRejectsProductionExampleBootstrapWithoutLeakingSentinelCredential(t *testing.T) {
	const sentinelCredential = "S0_02_08_SENTINEL_DO_NOT_LOG"
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "production")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "example")
	t.Setenv("IOT_DELIVERY_LOCAL_API_KEY", sentinelCredential)

	_, err := configurationFromEnv()
	if err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("production example bootstrap configuration error = %v, want generic bootstrap rejection", err)
	}
	if strings.Contains(err.Error(), sentinelCredential) {
		t.Fatalf("startup configuration error leaked sentinel credential: %q", err)
	}
}

func TestConfigurationRejectsUnknownRuntimeEnvironment(t *testing.T) {
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "preproductionish")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")

	_, err := configurationFromEnv()
	if err == nil || !strings.Contains(err.Error(), "runtime environment") {
		t.Fatalf("unknown runtime environment error = %v, want generic runtime environment rejection", err)
	}
}

func TestConfigurationRegistersOnlyExplicitExternalNotificationChannels(t *testing.T) {
	t.Setenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT", "development")
	t.Setenv("IOT_DELIVERY_BOOTSTRAP_MODE", "disabled")
	t.Setenv("IOT_DELIVERY_NOTIFICATION_WEBHOOK_URL", "https://hooks.example.test/delivery")
	t.Setenv("IOT_DELIVERY_NOTIFICATION_WECOM_WEBHOOK_URL", "https://wecom.example.test/robot")
	t.Setenv("IOT_DELIVERY_NOTIFICATION_SMTP_ADDRESS", "smtp.example.test:587")
	t.Setenv("IOT_DELIVERY_NOTIFICATION_SMTP_FROM", "delivery@example.test")
	t.Setenv("IOT_DELIVERY_NOTIFICATION_SMTP_TO", "team@example.test")

	configuration, err := configurationFromEnv()
	if err != nil {
		t.Fatalf("read configured notification channels: %v", err)
	}
	names := make([]string, 0, len(configuration.NotificationChannels))
	for _, channel := range configuration.NotificationChannels {
		names = append(names, channel.Name())
	}
	if strings.Join(names, ",") != "webhook,wecom-robot,email" {
		t.Fatalf("notification channels = %v, want webhook,wecom-robot,email", names)
	}
}
