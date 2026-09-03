package application_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"yunka.io/framework/core/identity"
	"yunka.io/framework/operation"
	"yunka.io/gateway/authz"
)

func TestOperationsUsesGeneratedPolicyForAuthorizationAndLocalOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox: %v", err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatalf("create local authorizer: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatalf("create execution security: %v", err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))
	operations := application.NewOperations(
		application.NewAdapter(service),
		operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}),
	)
	input := delivery.CreateInput{Title: "生成操作策略验收", Board: delivery.BoardResearchDelivery, Owner: "研发负责人"}

	viewer := identity.WithPrincipal(ctx, identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		Roles:         []string{localauth.RoleViewer},
	})
	if _, err := operations.Create(viewer, input); !authz.IsDenied(err) {
		t.Fatalf("viewer create error = %v, want authorization denial", err)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read denied-operation outbox: %v", err)
	}
	if snapshot.Pending != 0 {
		t.Fatalf("denied operation queued %d events, want 0", snapshot.Pending)
	}

	administrator := identity.WithPrincipal(ctx, identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		Roles:         []string{localauth.RoleLocalAdmin},
	})
	item, err := operations.Create(administrator, input)
	if err != nil {
		t.Fatalf("administrator create: %v", err)
	}
	if _, err := repository.Get(ctx, item.ID); err != nil {
		t.Fatalf("read committed item: %v", err)
	}
	snapshot, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read outbox after allowed operation: %v", err)
	}
	if snapshot.Pending != 1 {
		t.Fatalf("allowed operation queued %d events, want 1", snapshot.Pending)
	}
}

func TestOperationsListsNotificationsInsideTheYunkaReadTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := notification.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite notification store: %v", err)
	}
	if err := store.Save(ctx, notification.Notification{
		DeliveryID: "notification-read-001",
		Channel:    notification.LocalInboxChannelName,
		EventType:  "delivery.work-item.created",
		Subject:    "IOT-001",
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatalf("create local authorizer: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatalf("create execution security: %v", err)
	}
	service := delivery.NewService(repository, nil)
	operations := application.NewOperations(
		application.NewAdapter(service),
		operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}),
		service,
	).WithNotificationReader(store)
	administrator := identity.WithPrincipal(ctx, identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		Roles:         []string{localauth.RoleLocalAdmin},
	})

	result := make(chan struct {
		values []notification.Notification
		err    error
	}, 1)
	go func() {
		values, listErr := operations.ListNotifications(administrator, 10)
		result <- struct {
			values []notification.Notification
			err    error
		}{values: values, err: listErr}
	}()
	select {
	case listed := <-result:
		if listed.err != nil {
			t.Fatalf("list notifications through Yunka operation: %v", listed.err)
		}
		if len(listed.values) != 1 || listed.values[0].DeliveryID != "notification-read-001" {
			t.Fatalf("listed notifications = %#v, want seeded notification", listed.values)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listing notifications blocked inside the Yunka read transaction")
	}
}

func TestOperationsReadsProjectScheduleThroughTheYunkaReadScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatalf("create local authorizer: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatalf("create execution security: %v", err)
	}
	service := delivery.NewService(repository, nil)
	project, err := service.CreateProject(ctx, delivery.ProjectInput{Name: "项目排期读取", Board: delivery.BoardResearchDelivery, Owner: "研发负责人"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := service.Create(ctx, delivery.CreateInput{Title: "读取排期健康", Board: delivery.BoardResearchDelivery, ProjectID: project.ID, Kind: delivery.WorkItemKindTask, Owner: "研发负责人", EstimatePoints: 3}); err != nil {
		t.Fatalf("create work item: %v", err)
	}
	operations := application.NewOperations(
		application.NewAdapter(service),
		operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}),
		service,
	)
	viewer := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, Roles: []string{localauth.RoleViewer}})

	schedule, err := operations.ProjectSchedule(viewer, project.ID)
	if err != nil {
		t.Fatalf("read project schedule: %v", err)
	}
	if schedule.ProjectID != project.ID || schedule.TotalItems != 1 || len(schedule.Capacity) != 1 {
		t.Fatalf("project schedule = %#v, want one readable project item", schedule)
	}
}
