package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
)

func main() {
	configuration, err := configurationFromEnv()
	if err != nil {
		log.Fatalf("configure notification channels: %v", err)
	}
	application, err := bootstrap.New(context.Background(), configuration)
	if err != nil {
		log.Fatalf("configure iot delivery Yunka MVP: %v", err)
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := application.Close(shutdownContext); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("shutdown iot delivery Yunka MVP: %v", err)
		}
	}()

	log.Printf("iot delivery Yunka MVP listening on http://%s (gRPC %s)", application.HTTPAddress(), application.GRPCAddress())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
}

func configurationFromEnv() (bootstrap.Config, error) {
	startupPolicy, err := bootstrap.StartupPolicyFromEnvironment(os.Getenv)
	if err != nil {
		return bootstrap.Config{}, err
	}
	channels, err := notification.ChannelsFromEnvironment(os.Getenv)
	if err != nil {
		return bootstrap.Config{}, err
	}
	dueReminder, err := dueReminderConfigFromEnv()
	if err != nil {
		return bootstrap.Config{}, err
	}
	return startupPolicy.Apply(bootstrap.Config{
		HTTPAddress:            valueOr("IOT_DELIVERY_YUNKA_HTTP_ADDR", "127.0.0.1:8281"),
		GRPCAddress:            valueOr("IOT_DELIVERY_YUNKA_GRPC_ADDR", "127.0.0.1:8282"),
		DatabasePath:           valueOr("IOT_DELIVERY_YUNKA_DB", "data/iot-delivery-yunka.db"),
		ObsidianVault:          valueOr("IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT", "runtime-vault"),
		NotificationChannels:   channels,
		DueReminder:            dueReminder,
		BFFOrganizationID:      strings.TrimSpace(os.Getenv("IOT_DELIVERY_BFF_ORGANIZATION_ID")),
		BFFAssertionKey:        strings.TrimSpace(os.Getenv("IOT_DELIVERY_BFF_ASSERTION_KEY")),
		LocalAuthJWTSigningKey: strings.TrimSpace(os.Getenv("IOT_DELIVERY_LOCAL_AUTH_JWT_KEY")),
	}), nil
}

func dueReminderConfigFromEnv() (delivery.DueReminderConfig, error) {
	configuration := delivery.DueReminderConfig{LeadDays: 1, Interval: time.Hour}
	if raw := strings.TrimSpace(os.Getenv("IOT_DELIVERY_DUE_REMINDER_LEAD_DAYS")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return delivery.DueReminderConfig{}, errors.New("IOT_DELIVERY_DUE_REMINDER_LEAD_DAYS must be a non-negative integer")
		}
		configuration.LeadDays = value
	}
	if raw := strings.TrimSpace(os.Getenv("IOT_DELIVERY_DUE_REMINDER_INTERVAL")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return delivery.DueReminderConfig{}, errors.New("IOT_DELIVERY_DUE_REMINDER_INTERVAL must be a positive duration")
		}
		configuration.Interval = value
	}
	return configuration, nil
}

func valueOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
