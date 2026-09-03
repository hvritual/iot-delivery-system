package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend/internal/httpapi"
	"github.com/hvritual/iot-delivery-system/backend/internal/obsidian"
	"github.com/hvritual/iot-delivery-system/backend/internal/runtime"
)

type Config struct {
	Address       string
	DatabasePath  string
	ObsidianVault string
}

type Application struct {
	repository *delivery.SQLiteRepository
	service    *delivery.Service
	server     *runtime.Server
}

func New(ctx context.Context, configuration Config) (*Application, error) {
	configuration.Address = strings.TrimSpace(configuration.Address)
	configuration.DatabasePath = strings.TrimSpace(configuration.DatabasePath)
	configuration.ObsidianVault = strings.TrimSpace(configuration.ObsidianVault)
	if configuration.Address == "" || configuration.DatabasePath == "" || configuration.ObsidianVault == "" {
		return nil, errors.New("address, database path, and Obsidian vault are required")
	}
	repository, err := delivery.NewSQLiteRepository(configuration.DatabasePath)
	if err != nil {
		return nil, err
	}
	service := delivery.NewService(repository, obsidian.NewExporter(configuration.ObsidianVault))
	if err := seedExample(ctx, service); err != nil {
		_ = repository.Close()
		return nil, err
	}
	if err := service.Sync(ctx); err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("refresh Obsidian projection: %w", err)
	}
	server, err := runtime.NewServer(configuration.Address, httpapi.NewHandler(service))
	if err != nil {
		_ = repository.Close()
		return nil, err
	}
	return &Application{repository: repository, service: service, server: server}, nil
}

func (application *Application) Service() *delivery.Service {
	if application == nil {
		return nil
	}
	return application.service
}

func (application *Application) Start(ctx context.Context) error {
	if application == nil || application.server == nil {
		return errors.New("application is not configured")
	}
	return application.server.Start(ctx)
}

func (application *Application) Address() string {
	if application == nil || application.server == nil {
		return ""
	}
	return application.server.Address()
}

func (application *Application) Close(ctx context.Context) error {
	if application == nil {
		return nil
	}
	var failures []error
	if application.server != nil {
		failures = append(failures, application.server.Shutdown(ctx))
	}
	if application.repository != nil {
		failures = append(failures, application.repository.Close())
	}
	return errors.Join(failures...)
}

func seedExample(ctx context.Context, service *delivery.Service) error {
	items, err := service.List(ctx)
	if err != nil {
		return fmt.Errorf("inspect existing delivery items: %w", err)
	}
	if len(items) > 0 {
		return nil
	}
	item, err := service.Create(ctx, delivery.CreateInput{
		Title:    "样例：设备 OTA 发布验收",
		Board:    delivery.BoardResearchDelivery,
		Type:     "release",
		Owner:    "待分配",
		Priority: delivery.PriorityP0,
		Plan:     "验证分组灰度、回滚演练和发布验收证据。",
		Solution: "按设备分组推进灰度发布，并把回滚结果作为发布门禁证据。",
		IsSample: true,
	})
	if err != nil {
		return fmt.Errorf("seed sample delivery item: %w", err)
	}
	_, err = service.UpdateContext(ctx, item.ID, delivery.ContextUpdate{Decision: &delivery.Decision{
		Title:        "将回滚演练纳入发布门禁",
		Context:      "OTA 发布存在设备型号和网络差异。",
		Outcome:      "发布前必须附上灰度与回滚证据。",
		Consequences: "发布负责人需要维护证据链接。",
	}})
	if err != nil {
		return fmt.Errorf("seed sample decision: %w", err)
	}
	return nil
}
