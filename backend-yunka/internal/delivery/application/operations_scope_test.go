package application_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/policy"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/execution"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
	"github.com/hvritual/yunka.io/pkg/operationplan"
	_ "modernc.org/sqlite"
)

type preservingSecurity struct{}

func (preservingSecurity) Prepare(ctx context.Context, _ operationplan.Plan, _ any) (context.Context, error) {
	return ctx, nil
}

type recordingSecurity struct {
	plans []operationplan.Plan
}

func (security *recordingSecurity) Prepare(ctx context.Context, plan operationplan.Plan, _ any) (context.Context, error) {
	security.plans = append(security.plans, plan)
	return ctx, nil
}

type countingTransactionFactory struct {
	delegate execution.TransactionFactory
	begins   atomic.Int64
}

func (factory *countingTransactionFactory) Begin(ctx context.Context, mode execution.TransactionMode) (execution.UnitOfWork, error) {
	factory.begins.Add(1)
	return factory.delegate.Begin(ctx, mode)
}

type compositionObserver struct {
	roots    atomic.Int64
	children atomic.Int64
}

func (observer *compositionObserver) Observe(_ context.Context, event operation.Event) {
	if event.Phase != operation.PhasePlan || event.Outcome != operation.OutcomeStarted {
		return
	}
	if event.Kind == operation.InvocationRoot {
		observer.roots.Add(1)
	} else if event.Kind == operation.InvocationChild {
		observer.children.Add(1)
	}
}

func TestDashboardAndListUseCanonicalPlansWhenLegacyServiceIsAttached(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	service := delivery.NewService(repository, nil)
	security := &recordingSecurity{}
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	operations := application.NewOperations(application.NewAdapter(service), operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}), service)

	if _, err := operations.Dashboard(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := operations.List(t.Context()); err != nil {
		t.Fatal(err)
	}

	if len(security.plans) != 2 {
		t.Fatalf("prepared plans = %d, want 2", len(security.plans))
	}
	for index, want := range []operationplan.Plan{policy.OperationPlanGetDashboard(), policy.OperationPlanListItems()} {
		got := security.plans[index]
		if got.OperationID != want.OperationID || got.UseCase != want.UseCase || got.Security.Permissions[0] != want.Security.Permissions[0] {
			t.Errorf("prepared plan[%d] = id %q use case %q permissions %v, want canonical id %q use case %q permissions %v", index, got.OperationID, got.UseCase, got.Security.Permissions, want.OperationID, want.UseCase, want.Security.Permissions)
		}
	}
}

func TestCombinedUpdateUsesOneCanonicalRootAuthorizationAndUnitOfWork(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	now := time.Now().UTC()
	if err := repository.CreateProject(t.Context(), delivery.Project{ID: "project-a", OrganizationID: "org-a", Name: "A", Board: delivery.BoardResearchDelivery, Owner: "owner", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(t.Context(), delivery.WorkItem{ID: "item-a", ProjectID: "project-a", Board: delivery.BoardResearchDelivery, Revision: 1, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	security := &recordingSecurity{}
	factory := &countingTransactionFactory{delegate: localtx.NewSQLiteFactory(database)}
	observer := &compositionObserver{}
	service := delivery.NewService(repository, nil)
	operations := application.NewOperations(application.NewAdapter(service), operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: factory}, observer), service)
	progress := 40
	plan := "one root"

	item, err := operations.UpdateWorkItemAndContext(t.Context(), "item-a", 1, delivery.WorkItemUpdate{ProgressPercent: &progress}, delivery.ContextUpdate{Plan: &plan})
	if err != nil {
		t.Fatal(err)
	}
	if item.Revision != 3 || item.ProgressPercent != progress || item.Plan != "one root" {
		t.Fatalf("combined item = %#v, want both updates at revision 3", item)
	}
	if len(security.plans) != 1 || security.plans[0].OperationID != policy.OperationPlanUpdateItem().OperationID {
		t.Fatalf("security plans = %#v, want one canonical update root", security.plans)
	}
	if got := factory.begins.Load(); got != 1 {
		t.Fatalf("root UnitOfWork begins = %d, want 1", got)
	}
	if roots, children := observer.roots.Load(), observer.children.Load(); roots != 1 || children != 1 {
		t.Fatalf("execution plans = roots %d children %d, want 1/1", roots, children)
	}
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
