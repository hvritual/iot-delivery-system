package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hvritual/iot-delivery-system/backend/internal/bootstrap"
)

func main() {
	configuration := bootstrap.Config{
		Address:       valueOr("IOT_DELIVERY_ADDR", "127.0.0.1:8181"),
		DatabasePath:  valueOr("IOT_DELIVERY_DB", filepath.Join("data", "iot-delivery.db")),
		ObsidianVault: valueOr("IOT_DELIVERY_OBSIDIAN_VAULT", `F:\knowledge`),
	}
	application, err := bootstrap.New(context.Background(), configuration)
	if err != nil {
		log.Fatalf("configure iot delivery system: %v", err)
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := application.Close(shutdownContext); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("shutdown iot delivery system: %v", err)
		}
	}()
	if err := application.Start(context.Background()); err != nil {
		log.Fatalf("start iot delivery system: %v", err)
	}
	log.Printf("iot delivery system listening on http://%s", application.Address())

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
}

func valueOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
