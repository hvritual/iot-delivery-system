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
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
	"github.com/hvritual/yunka.io/pkg/operationplan"
	_ "modernc.org/sqlite"
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

func TestCanonicalItemReadsEnforceObjectAndAuthorizedProjectScope(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	now := time.Now().UTC()
	for _, project := range []delivery.Project{
		{ID: "project-a", OrganizationID: "org-a", Name: "A", Board: delivery.BoardResearchDelivery, Owner: "owner", CreatedAt: now, UpdatedAt: now},
		{ID: "project-b", OrganizationID: "org-a", Name: "B", Board: delivery.BoardResearchDelivery, Owner: "owner", CreatedAt: now, UpdatedAt: now},
	} {
		if err := repository.CreateProject(t.Context(), project); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []delivery.WorkItem{
		{ID: "item-a", Title: "canonical exact", Board: delivery.BoardResearchDelivery, ProjectID: "project-a", Kind: delivery.WorkItemKindTask, UpdatedAt: now},
		{ID: "item-b", Title: "canonical exact", Board: delivery.BoardResearchDelivery, ProjectID: "project-b", Kind: delivery.WorkItemKindTask, UpdatedAt: now},
	} {
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
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	guard, err := deliveryauthz.NewOperationGuard(repository, database)
	if err != nil {
		t.Fatal(err)
	}
	principal := identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a"}
	grant := authz.Grant{Permission: "delivery.work-items.read", RoleID: "viewer", Scope: "project:project-a"}
	authorized := func(operationID string) authz.AuthorizedOperation {
		return authz.AuthorizedOperation{
			Principal: principal,
			Policy:    authz.Policy{Operation: authz.OperationID(operationID), Permissions: []authz.PermissionKey{"delivery.work-items.read"}},
			Decision:  authz.Decision{Allowed: true, Operation: authz.OperationID(operationID), Permissions: []authz.PermissionKey{"delivery.work-items.read"}, Grants: []authz.Grant{grant}},
		}
	}
	service := delivery.NewService(repository, nil)
	operations := application.NewOperations(application.NewAdapter(service), operation.NewExecutorWithOptions(preservingSecurity{}, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}))

	getContext, err := guard.Prepare(t.Context(), authorized("delivery.items.get"), &deliveryv1.GetItemRequest{Id: "item-a"})
	if err != nil {
		t.Fatal(err)
	}
	if item, err := operations.Get(getContext, "item-a"); err != nil || item.ID != "item-a" {
		t.Fatalf("authorized get = %#v, %v", item, err)
	}
	if _, err := guard.Prepare(t.Context(), authorized("delivery.items.get"), &deliveryv1.GetItemRequest{Id: "item-b"}); !authz.IsDenied(err) {
		t.Fatalf("other-project get error = %v, want denial", err)
	}

	searchContext, err := guard.Prepare(t.Context(), authorized("delivery.items.search"), &deliveryv1.SearchItemsRequest{Query: "canonical"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := operations.Search(searchContext, delivery.WorkItemFilter{Query: "canonical"})
	if err != nil || len(items) != 1 || items[0].ID != "item-a" {
		t.Fatalf("authorized search = %#v, %v", items, err)
	}
	if _, err := guard.Prepare(t.Context(), authorized("delivery.items.search"), &deliveryv1.SearchItemsRequest{ProjectId: "project-b"}); !authz.IsDenied(err) {
		t.Fatalf("other-project search error = %v, want denial", err)
	}

	similarContext, err := guard.Prepare(t.Context(), authorized("delivery.items.similarity"), &deliveryv1.FindSimilarItemsRequest{ProjectId: "project-a", Title: "canonical exact", Kind: string(delivery.WorkItemKindTask)})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := operations.FindSimilar(similarContext, delivery.SimilarityQuery{ProjectID: "project-a", Title: "canonical exact", Kind: delivery.WorkItemKindTask})
	if err != nil || len(candidates) != 1 || candidates[0].ID != "item-a" || !candidates[0].Exact {
		t.Fatalf("authorized similarity = %#v, %v", candidates, err)
	}
	if _, err := guard.Prepare(t.Context(), authorized("delivery.items.similarity"), &deliveryv1.FindSimilarItemsRequest{ProjectId: "project-b", Title: "canonical exact"}); !authz.IsDenied(err) {
		t.Fatalf("other-project similarity error = %v, want denial", err)
	}
}
