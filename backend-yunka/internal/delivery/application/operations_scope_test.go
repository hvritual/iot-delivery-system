package application_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	_ "modernc.org/sqlite"
	"yunka.io/framework/core/identity"
	"yunka.io/framework/operation"
	"yunka.io/gateway/authz"
	"yunka.io/pkg/operationplan"
)

type preservingSecurity struct{}

func (preservingSecurity) Prepare(ctx context.Context, _ operationplan.Plan, _ any) (context.Context, error) {
	return ctx, nil
}

func TestOperationsListDoesNotBypassGuardProjectSet(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	now := time.Now().UTC()
	for _, project := range []delivery.Project{{ID: "project-a", OrganizationID: "org-a", Name: "A", Board: delivery.BoardResearchDelivery, Owner: "owner", CreatedAt: now, UpdatedAt: now}, {ID: "project-b", OrganizationID: "org-a", Name: "B", Board: delivery.BoardResearchDelivery, Owner: "owner", CreatedAt: now, UpdatedAt: now}} {
		if err := repository.CreateProject(t.Context(), project); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []delivery.WorkItem{{ID: "item-a", ProjectID: "project-a", UpdatedAt: now}, {ID: "item-b", ProjectID: "project-b", UpdatedAt: now}} {
		if err := repository.Create(t.Context(), item); err != nil {
			t.Fatal(err)
		}
	}
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name) VALUES ('org-a', 'org-a', 'Organization A')`,
		`INSERT INTO users (id, organization_id, display_name) VALUES ('user-a', 'org-a', 'Alice')`,
		`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id) VALUES ('binding-viewer-a', 'org-a', 'viewer', 'project', 'project-a', 'user-a')`,
		`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id) VALUES ('binding-viewer-b', 'org-a', 'viewer', 'project', 'project-b', 'user-a')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	guard, err := deliveryauthz.NewOperationGuard(repository, database)
	if err != nil {
		t.Fatal(err)
	}
	secured, err := guard.Prepare(t.Context(), authz.AuthorizedOperation{Principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a"}, Policy: authz.Policy{Operation: "delivery.items.list", Permissions: []authz.PermissionKey{"delivery.work-items.read"}}, Decision: authz.Decision{Allowed: true, Operation: "delivery.items.list", Permissions: []authz.PermissionKey{"delivery.work-items.read"}, Grants: []authz.Grant{{Permission: "delivery.work-items.read", RoleID: "viewer", Scope: "project:project-a"}, {Permission: "delivery.work-items.read", RoleID: "viewer", Scope: "project:project-b"}}}}, &deliveryv1.ListItemsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	service := delivery.NewService(repository, nil)
	operations := application.NewOperations(application.NewAdapter(service), operation.NewExecutorWithOptions(preservingSecurity{}, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}), service)
	items, err := operations.List(secured)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "item-a" || items[1].ID != "item-b" {
		t.Fatalf("listed items = %#v, want item-a and item-b", items)
	}
}
