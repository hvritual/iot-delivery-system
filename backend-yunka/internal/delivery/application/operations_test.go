package application_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
)

type generatedGovernanceWriteSpy struct {
	application.DeliveryService
	updateContextCalls int
	advanceGateCalls   int
	closeCalls         int
}

func (spy *generatedGovernanceWriteSpy) UpdateItemContext(ctx context.Context, request *deliveryv1.UpdateItemContextRequest) (*deliveryv1.WorkItemResponse, error) {
	spy.updateContextCalls++
	return spy.DeliveryService.UpdateItemContext(ctx, request)
}

func (spy *generatedGovernanceWriteSpy) AdvanceGate(ctx context.Context, request *deliveryv1.AdvanceGateRequest) (*deliveryv1.WorkItemResponse, error) {
	spy.advanceGateCalls++
	return spy.DeliveryService.AdvanceGate(ctx, request)
}

func (spy *generatedGovernanceWriteSpy) CloseItem(ctx context.Context, request *deliveryv1.CloseItemRequest) (*deliveryv1.WorkItemResponse, error) {
	spy.closeCalls++
	return spy.DeliveryService.CloseItem(ctx, request)
}

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

func TestOperationsItemReadsDoNotRequireLegacyExtensionService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))
	operations := application.NewOperations(
		application.NewAdapter(service),
		operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}),
	)
	administrator := identity.WithPrincipal(ctx, identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		TenantID:      localauth.DevelopmentTenantID,
		UserID:        "canonical-admin",
		Subject:       "display-name-must-not-own-views",
		Roles:         []string{localauth.RoleLocalAdmin},
	})
	project, err := operations.CreateProject(administrator, delivery.ProjectInput{Name: "item reads", Board: delivery.BoardResearchDelivery, Owner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := operations.Create(administrator, delivery.CreateInput{Title: "canonical exact title", Board: delivery.BoardResearchDelivery, ProjectID: project.ID, Owner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := operations.Get(administrator, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("get item = %#v, %v", got, err)
	}
	items, err := operations.Search(administrator, delivery.WorkItemFilter{ProjectID: project.ID, Query: "exact title"})
	if err != nil || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("search items = %#v, %v", items, err)
	}
	candidates, err := operations.FindSimilar(administrator, delivery.SimilarityQuery{ProjectID: project.ID, Title: created.Title, Kind: created.Kind})
	if err != nil || len(candidates) != 1 || candidates[0].ID != created.ID || !candidates[0].Exact {
		t.Fatalf("similar items = %#v, %v", candidates, err)
	}
	view, err := operations.SaveView(administrator, delivery.SavedViewInput{Name: "canonical view", Filter: delivery.WorkItemFilter{ProjectID: project.ID}})
	if err != nil || view.Owner != "canonical-admin" {
		t.Fatalf("save view = %#v, %v; want canonical UserID owner", view, err)
	}
	views, err := operations.ListSavedViews(administrator)
	if err != nil || len(views) != 1 || views[0].ID != view.ID {
		t.Fatalf("list saved views = %#v, %v", views, err)
	}
	week, err := operations.MemberWeek(administrator, "owner", "2026-09-07")
	if err != nil || week.Member != "owner" {
		t.Fatalf("member week = %#v, %v", week, err)
	}
}

func TestOperationsCreatePreservesNestedWriteContract(t *testing.T) {
	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))
	operations := application.NewOperations(application.NewAdapter(service), operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}))
	admin := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: localauth.DevelopmentTenantID, Roles: []string{localauth.RoleLocalAdmin}})
	project, err := operations.CreateProject(admin, delivery.ProjectInput{Name: "nested", Board: delivery.BoardResearchDelivery, Owner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	release, err := operations.CreateRelease(admin, delivery.ReleaseInput{ProjectID: project.ID, Name: "release", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	sprint, err := operations.CreateSprint(admin, delivery.SprintInput{ProjectID: project.ID, Name: "sprint", StartDate: "2026-09-01", EndDate: "2026-09-10"})
	if err != nil {
		t.Fatal(err)
	}
	milestone, err := operations.CreateMilestone(admin, delivery.MilestoneInput{ProjectID: project.ID, Name: "milestone", TargetDate: "2026-09-10"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := operations.Create(admin, delivery.CreateInput{Title: "parent", Board: delivery.BoardResearchDelivery, ProjectID: project.ID, Kind: delivery.WorkItemKindEpic, Owner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := operations.Create(admin, delivery.CreateInput{Title: "dependency", Board: delivery.BoardResearchDelivery, ProjectID: project.ID, Owner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	recordedAt := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	item, err := operations.Create(admin, delivery.CreateInput{Title: "target", Board: delivery.BoardResearchDelivery, ProjectID: project.ID, ParentID: parent.ID, Kind: delivery.WorkItemKindSubtask, Owner: "owner", Type: "defect", Priority: delivery.PriorityP0, ReleaseID: release.ID, SprintID: sprint.ID, MilestoneID: milestone.ID, StartDate: "2026-09-02", DueDate: "2026-09-09", EstimatePoints: 3.5, ProgressPercent: 40, Plan: "plan", Solution: "solution", IsSample: true, Dependencies: []delivery.WorkItemDependency{{ItemID: dependency.ID, Relation: delivery.DependencyDependsOn}}, IoTBindings: []delivery.IoTBinding{{Kind: delivery.IoTBindingDevice, Reference: "SN-1", Label: "device", Attributes: map[string]string{"site": "A"}}}, TraceLinks: []delivery.TraceLink{{Kind: delivery.TraceBuild, Reference: "build-1", Title: "build", URL: "https://example.test/build-1", Status: "passed", RecordedAt: recordedAt}}})
	if err != nil {
		t.Fatal(err)
	}
	if item.ProjectID != project.ID || item.ParentID != parent.ID || item.Kind != delivery.WorkItemKindSubtask || item.ReleaseID != release.ID || item.SprintID != sprint.ID || item.MilestoneID != milestone.ID || item.StartDate != "2026-09-02" || item.DueDate != "2026-09-09" || item.EstimatePoints != 3.5 || item.ProgressPercent != 40 || item.Plan != "plan" || item.Solution != "solution" || !item.IsSample || len(item.Dependencies) != 1 || len(item.IoTBindings) != 1 || item.IoTBindings[0].Attributes["site"] != "A" || len(item.TraceLinks) != 1 {
		t.Fatalf("generated create lost nested fields: %#v", item)
	}
	stored, err := repository.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "target" || item.Board != delivery.BoardResearchDelivery || item.Owner != "owner" || item.Type != "defect" || item.Priority != delivery.PriorityP0 || item.Dependencies[0].ItemID != dependency.ID || item.Dependencies[0].Relation != delivery.DependencyDependsOn || item.IoTBindings[0].Kind != delivery.IoTBindingDevice || item.IoTBindings[0].Reference != "SN-1" || item.IoTBindings[0].Label != "device" || item.TraceLinks[0].Kind != delivery.TraceBuild || item.TraceLinks[0].Title != "build" || item.TraceLinks[0].URL != "https://example.test/build-1" || item.TraceLinks[0].Status != "passed" || !item.TraceLinks[0].RecordedAt.Equal(recordedAt) {
		t.Fatalf("create response lost fields: %#v", item)
	}
	if stored.Title != item.Title || stored.Board != item.Board || stored.Owner != item.Owner || stored.Type != item.Type || stored.Priority != item.Priority || stored.Kind != item.Kind || stored.ProjectID != item.ProjectID || stored.ParentID != item.ParentID || stored.ReleaseID != item.ReleaseID || stored.SprintID != item.SprintID || stored.MilestoneID != item.MilestoneID || stored.StartDate != item.StartDate || stored.DueDate != item.DueDate || stored.EstimatePoints != item.EstimatePoints || stored.ProgressPercent != item.ProgressPercent || stored.Plan != item.Plan || stored.Solution != item.Solution || stored.IsSample != item.IsSample || len(stored.Dependencies) != 1 || stored.Dependencies[0] != item.Dependencies[0] || len(stored.IoTBindings) != 1 || stored.IoTBindings[0].Kind != delivery.IoTBindingDevice || stored.IoTBindings[0].Reference != "SN-1" || stored.IoTBindings[0].Label != "device" || stored.IoTBindings[0].Attributes["site"] != "A" || len(stored.TraceLinks) != 1 || stored.TraceLinks[0].Kind != delivery.TraceBuild || stored.TraceLinks[0].Reference != "build-1" || stored.TraceLinks[0].Title != "build" || stored.TraceLinks[0].URL != "https://example.test/build-1" || stored.TraceLinks[0].Status != "passed" || !stored.TraceLinks[0].RecordedAt.Equal(recordedAt) {
		t.Fatalf("stored create lost fields: %#v", stored)
	}
}

func TestOperationsUpdatePresenceAndCommentPersistThroughGeneratedWrites(t *testing.T) {
	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))
	operations := application.NewOperations(application.NewAdapter(service), operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}))
	admin := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: localauth.DevelopmentTenantID, UserID: "actor", Roles: []string{localauth.RoleLocalAdmin}})
	project, err := operations.CreateProject(admin, delivery.ProjectInput{Name: "presence", Board: delivery.BoardResearchDelivery, Owner: "actor"})
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := operations.Create(admin, delivery.CreateInput{Title: "dep", Board: delivery.BoardResearchDelivery, ProjectID: project.ID, Owner: "actor"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := operations.Create(admin, delivery.CreateInput{Title: "item", Board: delivery.BoardResearchDelivery, ProjectID: project.ID, Owner: "actor", Dependencies: []delivery.WorkItemDependency{{ItemID: dependency.ID, Relation: delivery.DependencyDependsOn}}})
	if err != nil {
		t.Fatal(err)
	}
	empty := []delivery.WorkItemDependency{}
	updated, err := operations.UpdateWorkItem(admin, item.ID, currentRevision(t, operations, admin, item.ID), delivery.WorkItemUpdate{Dependencies: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Dependencies) != 0 {
		t.Fatalf("explicit empty dependencies=%#v", updated.Dependencies)
	}
	title := "renamed"
	unchanged, err := operations.UpdateWorkItem(admin, item.ID, currentRevision(t, operations, admin, item.ID), delivery.WorkItemUpdate{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged.Dependencies) != 0 {
		t.Fatalf("omitted dependencies changed=%#v", unchanged.Dependencies)
	}
	beforeComment, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	comment, err := operations.AddComment(admin, item.ID, currentRevision(t, operations, admin, item.ID), delivery.CommentInput{Body: "comment"})
	if err != nil {
		t.Fatal(err)
	}
	if comment.Author != "actor" || comment.CreatedAt.IsZero() {
		t.Fatalf("comment=%#v", comment)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil || snapshot.Pending != beforeComment.Pending+1 {
		t.Fatalf("comment outbox=%#v err=%v", snapshot, err)
	}
	afterCommentTitle := "after-comment"
	response, err := operations.UpdateWorkItem(admin, item.ID, currentRevision(t, operations, admin, item.ID), delivery.WorkItemUpdate{Title: &afterCommentTitle})
	if err != nil || len(response.Comments) != 1 || len(response.Activities) == 0 || response.Comments[0].Author != "actor" || response.Comments[0].CreatedAt.IsZero() || response.Activities[len(response.Activities)-1].OccurredAt.IsZero() {
		t.Fatalf("work item response lost comment/activity: %#v err=%v", response, err)
	}
	stored, err := repository.Get(ctx, item.ID)
	if err != nil || len(stored.Comments) != 1 {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestOperationsRejectsInvalidDependenciesWithoutOutboxSideEffects(t *testing.T) {
	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))
	operations := application.NewOperations(application.NewAdapter(service), operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}))
	admin := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: localauth.DevelopmentTenantID, Roles: []string{localauth.RoleLocalAdmin}})
	p1, err := operations.CreateProject(admin, delivery.ProjectInput{Name: "p1", Board: delivery.BoardResearchDelivery, Owner: "o"})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := operations.CreateProject(admin, delivery.ProjectInput{Name: "p2", Board: delivery.BoardResearchDelivery, Owner: "o"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := operations.Create(admin, delivery.CreateInput{Title: "foreign", Board: delivery.BoardResearchDelivery, ProjectID: p2.ID, Owner: "o"})
	if err != nil {
		t.Fatal(err)
	}
	local, err := operations.Create(admin, delivery.CreateInput{Title: "local", Board: delivery.BoardResearchDelivery, ProjectID: p1.ID, Owner: "o"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deps := []delivery.WorkItemDependency{{ItemID: foreign.ID, Relation: delivery.DependencyDependsOn}}
	if _, err := operations.UpdateWorkItem(admin, local.ID, currentRevision(t, operations, admin, local.ID), delivery.WorkItemUpdate{Dependencies: &deps}); !errors.Is(err, delivery.ErrProjectParentMismatch) {
		t.Fatalf("cross-project error=%v", err)
	}
	after, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Pending != before.Pending || len(stored.Dependencies) != 0 {
		t.Fatalf("cross project side effect outbox=%#v stored=%#v", after, stored)
	}
	deps = []delivery.WorkItemDependency{{ItemID: local.ID, Relation: delivery.DependencyDependsOn}}
	if _, err := operations.UpdateWorkItem(admin, local.ID, currentRevision(t, operations, admin, local.ID), delivery.WorkItemUpdate{Dependencies: &deps}); !errors.Is(err, delivery.ErrCircularDependency) {
		t.Fatalf("self-cycle error=%v", err)
	}
	after, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored, err = repository.Get(ctx, local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Pending != before.Pending || len(stored.Dependencies) != 0 {
		t.Fatalf("cycle side effect outbox=%#v stored=%#v", after, stored)
	}
	a, err := operations.Create(admin, delivery.CreateInput{Title: "a", Board: delivery.BoardResearchDelivery, ProjectID: p1.ID, Owner: "o"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := operations.Create(admin, delivery.CreateInput{Title: "b", Board: delivery.BoardResearchDelivery, ProjectID: p1.ID, Owner: "o"})
	if err != nil {
		t.Fatal(err)
	}
	deps = []delivery.WorkItemDependency{{ItemID: b.ID, Relation: delivery.DependencyDependsOn}}
	if _, err := operations.UpdateWorkItem(admin, a.ID, currentRevision(t, operations, admin, a.ID), delivery.WorkItemUpdate{Dependencies: &deps}); err != nil {
		t.Fatal(err)
	}
	before, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deps = []delivery.WorkItemDependency{{ItemID: a.ID, Relation: delivery.DependencyDependsOn}}
	if _, err := operations.UpdateWorkItem(admin, b.ID, currentRevision(t, operations, admin, b.ID), delivery.WorkItemUpdate{Dependencies: &deps}); !errors.Is(err, delivery.ErrCircularDependency) {
		t.Fatalf("two-node error=%v", err)
	}
	after, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored, err = repository.Get(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Pending != before.Pending || len(stored.Dependencies) != 0 {
		t.Fatalf("two-node side effect outbox=%#v stored=%#v", after, stored)
	}
}

func TestOperationsViewerWriteOperationsHaveNoSideEffects(t *testing.T) {
	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))
	operations := application.NewOperations(application.NewAdapter(service), operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}))
	admin := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, Roles: []string{localauth.RoleLocalAdmin}})
	viewer := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, Roles: []string{localauth.RoleViewer}})
	item, err := operations.Create(admin, delivery.CreateInput{Title: "existing", Board: delivery.BoardResearchDelivery, Owner: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeItems, err := repository.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	title := "changed"
	calls := []func() error{func() error {
		_, e := operations.Create(viewer, delivery.CreateInput{Title: "denied", Board: delivery.BoardResearchDelivery, Owner: "viewer"})
		return e
	}, func() error {
		_, e := operations.UpdateWorkItem(viewer, item.ID, currentRevision(t, operations, viewer, item.ID), delivery.WorkItemUpdate{Title: &title})
		return e
	}, func() error {
		_, e := operations.AddComment(viewer, item.ID, currentRevision(t, operations, viewer, item.ID), delivery.CommentInput{Body: "denied"})
		return e
	}}
	for _, call := range calls {
		if err := call(); !authz.IsDenied(err) {
			t.Fatalf("viewer error=%v", err)
		}
	}
	after, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterItems, err := repository.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Pending != before.Pending || len(afterItems) != len(beforeItems) || stored.Title != "existing" || len(stored.Comments) != 0 {
		t.Fatalf("viewer side effect outbox=%#v item=%#v", after, stored)
	}
}

func TestOperationsGovernanceWritesUseGeneratedContractsWhenServiceIsProvided(t *testing.T) {
	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))
	applicationPort := &generatedGovernanceWriteSpy{DeliveryService: application.NewAdapter(service)}
	operations := application.NewOperations(
		applicationPort,
		operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}),
		service,
	)
	administrator := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "local-development", UserID: "governance-actor", Roles: []string{localauth.RoleLocalAdmin}})
	reviewer := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "local-development", UserID: "governance-reviewer", Roles: []string{localauth.RoleLocalAdmin}})
	viewer := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, Roles: []string{localauth.RoleViewer}})

	item, err := operations.Create(administrator, delivery.CreateInput{Title: "generated governance writes", Board: delivery.BoardResearchDelivery, Owner: "governance-actor", Plan: "keep", Solution: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	beforeUpdate, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	decisionTime := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	decision := delivery.Decision{ID: "ADR-caller-001", Title: " preserve all decision fields ", Context: " context ", Outcome: " outcome ", Consequences: " consequences ", CreatedAt: decisionTime}
	updated, err := operations.UpdateContext(administrator, item.ID, currentRevision(t, operations, administrator, item.ID), delivery.ContextUpdate{Plan: &empty, Solution: &empty, Blocker: &empty, Decision: &decision})
	if err != nil {
		t.Fatal(err)
	}
	if applicationPort.updateContextCalls != 1 {
		t.Fatalf("generated UpdateItemContext calls = %d, want 1", applicationPort.updateContextCalls)
	}
	if updated.Plan != "" || updated.Solution != "" || updated.Blocker != "" || len(updated.Decisions) != 1 || updated.Decisions[0] != (delivery.Decision{ID: "ADR-caller-001", Title: "preserve all decision fields", Context: "context", Outcome: "outcome", Consequences: "consequences", CreatedAt: decisionTime}) || updated.UpdatedAt.IsZero() || len(updated.Activities) == 0 || updated.Activities[len(updated.Activities)-1].Actor != "governance-actor" {
		t.Fatalf("updated context = %#v", updated)
	}
	persisted, err := repository.Get(ctx, item.ID)
	if err != nil || !reflect.DeepEqual(persisted, updated) {
		t.Fatalf("persisted context = %#v, %v; want %#v", persisted, err, updated)
	}
	afterUpdate, err := store.Snapshot(ctx)
	if err != nil || afterUpdate.Pending != beforeUpdate.Pending+1 {
		t.Fatalf("context update outbox = %#v, %v", afterUpdate, err)
	}

	assertNoSideEffect := func(name string, want error, call func() error) {
		t.Helper()
		beforeItem, snapshotErr := repository.Get(ctx, item.ID)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		beforeOutbox, snapshotErr := store.Snapshot(ctx)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if callErr := call(); !errors.Is(callErr, want) {
			t.Fatalf("%s error = %v, want %v", name, callErr, want)
		}
		afterItem, snapshotErr := repository.Get(ctx, item.ID)
		if snapshotErr != nil || !reflect.DeepEqual(afterItem, beforeItem) {
			t.Fatalf("%s changed item = %#v, %v; want %#v", name, afterItem, snapshotErr, beforeItem)
		}
		afterOutbox, snapshotErr := store.Snapshot(ctx)
		if snapshotErr != nil || afterOutbox != beforeOutbox {
			t.Fatalf("%s changed outbox = %#v, %v; want %#v", name, afterOutbox, snapshotErr, beforeOutbox)
		}
	}
	assertNoSideEffect("close before production validation", delivery.ErrReleaseNotValidated, func() error {
		_, callErr := operations.Close(administrator, item.ID, currentRevision(t, operations, administrator, item.ID), "retrospective")
		return callErr
	})
	assertNoSideEffect("advance without evidence", delivery.ErrEvidenceRequired, func() error {
		_, callErr := operations.AdvanceGate(administrator, item.ID, currentRevision(t, operations, administrator, item.ID), delivery.GateSolutionReviewed, nil)
		return callErr
	})
	assertNoSideEffect("advance with incomplete evidence", delivery.ErrEvidenceRequired, func() error {
		_, callErr := operations.AdvanceGate(administrator, item.ID, currentRevision(t, operations, administrator, item.ID), delivery.GateSolutionReviewed, []delivery.Evidence{{Kind: "test"}})
		return callErr
	})

	beforeAdvance, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := operations.AdvanceGate(administrator, item.ID, currentRevision(t, operations, administrator, item.ID), delivery.GateSolutionReviewed, []delivery.Evidence{{Kind: "review", Title: "solution approved", Reference: "ADR-caller-001"}})
	if err != nil {
		t.Fatal(err)
	}
	if applicationPort.advanceGateCalls != 3 || advanced.Gate != delivery.GateSolutionReviewed || advanced.Status != delivery.StatusInProgress || len(advanced.Evidence) != 1 || advanced.Evidence[0].RecordedAt.IsZero() || len(advanced.Activities) == 0 || advanced.Activities[len(advanced.Activities)-1].Actor != "governance-actor" {
		t.Fatalf("advanced item = %#v, generated calls = %d", advanced, applicationPort.advanceGateCalls)
	}
	persisted, err = repository.Get(ctx, item.ID)
	if err != nil || !reflect.DeepEqual(persisted, advanced) {
		t.Fatalf("persisted advanced item = %#v, %v; want %#v", persisted, err, advanced)
	}
	afterAdvance, err := store.Snapshot(ctx)
	if err != nil || afterAdvance.Pending != beforeAdvance.Pending+1 {
		t.Fatalf("advance outbox = %#v, %v", afterAdvance, err)
	}
	assertNoSideEffect("skip a delivery gate", delivery.ErrInvalidGateTransition, func() error {
		_, callErr := operations.AdvanceGate(administrator, item.ID, currentRevision(t, operations, administrator, item.ID), delivery.GateTestPassed, []delivery.Evidence{{Kind: "test", Title: "skip"}})
		return callErr
	})

	for _, gate := range []delivery.Gate{delivery.GateDevelopmentCompleted, delivery.GateTestPassed, delivery.GateProductionValidated} {
		beforeGate, snapshotErr := store.Snapshot(ctx)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		actor := administrator
		if gate == delivery.GateProductionValidated {
			actor = reviewer
		}
		advanced, err = operations.AdvanceGate(actor, item.ID, currentRevision(t, operations, actor, item.ID), gate, []delivery.Evidence{{Kind: "test", Title: string(gate)}})
		if err != nil {
			t.Fatalf("advance %s: %v", gate, err)
		}
		afterGate, snapshotErr := store.Snapshot(ctx)
		if snapshotErr != nil || afterGate.Pending != beforeGate.Pending+1 {
			t.Fatalf("advance %s outbox = %#v, %v", gate, afterGate, snapshotErr)
		}
	}
	if applicationPort.advanceGateCalls != 7 || advanced.Gate != delivery.GateProductionValidated || len(advanced.Evidence) != 4 {
		t.Fatalf("generated advance calls = %d, item = %#v", applicationPort.advanceGateCalls, advanced)
	}
	assertNoSideEffect("close without retrospective", delivery.ErrRetrospectiveRequired, func() error {
		_, callErr := operations.Close(reviewer, item.ID, currentRevision(t, operations, reviewer, item.ID), " \t ")
		return callErr
	})
	beforeClose, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := operations.Close(reviewer, item.ID, currentRevision(t, operations, reviewer, item.ID), "  retained retrospective  ")
	if err != nil {
		t.Fatal(err)
	}
	if applicationPort.closeCalls != 3 || closed.Status != delivery.StatusClosed || closed.Retrospective != "retained retrospective" || len(closed.Activities) == 0 || closed.Activities[len(closed.Activities)-1].Actor != "governance-reviewer" {
		t.Fatalf("closed item = %#v, generated calls = %d", closed, applicationPort.closeCalls)
	}
	persisted, err = repository.Get(ctx, item.ID)
	visiblePersisted := persisted
	visiblePersisted.ImplementationPrincipal = delivery.PrincipalSource{}
	visiblePersisted.ProductionValidationPrincipal = delivery.PrincipalSource{}
	if err != nil || !reflect.DeepEqual(visiblePersisted, closed) {
		t.Fatalf("persisted closed item = %#v, %v; want %#v", persisted, err, closed)
	}
	if persisted.ImplementationPrincipal.SubjectID != "governance-actor" || persisted.ProductionValidationPrincipal.SubjectID != "governance-reviewer" {
		t.Fatalf("persisted segregation-of-duties principals = %#v / %#v", persisted.ImplementationPrincipal, persisted.ProductionValidationPrincipal)
	}
	afterClose, err := store.Snapshot(ctx)
	if err != nil || afterClose.Pending != beforeClose.Pending+1 {
		t.Fatalf("close outbox = %#v, %v", afterClose, err)
	}

	viewerCalls := applicationPort.updateContextCalls + applicationPort.advanceGateCalls + applicationPort.closeCalls
	errViewerCallsDenied := errors.New("viewer calls were all denied")
	assertNoSideEffect("viewer generated governance writes", errViewerCallsDenied, func() error {
		if _, callErr := operations.UpdateContext(viewer, item.ID, currentRevision(t, operations, viewer, item.ID), delivery.ContextUpdate{Plan: &empty}); !authz.IsDenied(callErr) {
			return callErr
		}
		if _, callErr := operations.AdvanceGate(viewer, item.ID, currentRevision(t, operations, viewer, item.ID), delivery.GateProductionValidated, []delivery.Evidence{{Kind: "test", Title: "denied"}}); !authz.IsDenied(callErr) {
			return callErr
		}
		if _, callErr := operations.Close(viewer, item.ID, currentRevision(t, operations, viewer, item.ID), "denied"); !authz.IsDenied(callErr) {
			return callErr
		}
		return errViewerCallsDenied
	})
	if got := applicationPort.updateContextCalls + applicationPort.advanceGateCalls + applicationPort.closeCalls; got != viewerCalls {
		t.Fatalf("denied viewer call reached generated application: got %d calls, want %d", got, viewerCalls)
	}
}

func TestOperationsRejectsOutOfRangeProgressBeforeWriting(t *testing.T) {
	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatal(err)
	}
	operations := application.NewOperations(application.NewAdapter(delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))), operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}))
	admin := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, Roles: []string{localauth.RoleLocalAdmin}})
	before, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operations.Create(admin, delivery.CreateInput{Title: "bad", Board: delivery.BoardResearchDelivery, Owner: "owner", ProgressPercent: 101}); err == nil {
		t.Fatal("out of range create accepted")
	}
	items, err := repository.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.Snapshot(ctx)
	if err != nil || len(items) != 0 || after.Pending != before.Pending {
		t.Fatalf("invalid create side effect items=%#v outbox=%#v err=%v", items, after, err)
	}
	item, err := operations.Create(admin, delivery.CreateInput{Title: "valid", Board: delivery.BoardResearchDelivery, Owner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	before, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	invalid := 101
	if _, err := operations.UpdateWorkItem(admin, item.ID, currentRevision(t, operations, admin, item.ID), delivery.WorkItemUpdate{ProgressPercent: &invalid}); err == nil {
		t.Fatal("out of range update accepted")
	}
	stored, err := repository.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err = store.Snapshot(ctx)
	if err != nil || stored.ProgressPercent != 0 || after.Pending != before.Pending {
		t.Fatalf("invalid update side effect item=%#v outbox=%#v err=%v", stored, after, err)
	}
}

func TestOperationsUpdateAllFieldPresenceThroughGeneratedContract(t *testing.T) {
	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := delivery.NewService(repository, nil)
	operations := application.NewOperations(application.NewAdapter(service), operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}))
	admin := identity.WithPrincipal(ctx, identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: localauth.DevelopmentTenantID, Roles: []string{localauth.RoleLocalAdmin}})
	project, err := operations.CreateProject(admin, delivery.ProjectInput{Name: "project", Board: delivery.BoardResearchDelivery, Owner: "old"})
	if err != nil {
		t.Fatal(err)
	}
	release, err := operations.CreateRelease(admin, delivery.ReleaseInput{ProjectID: project.ID, Name: "release", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	sprint, err := operations.CreateSprint(admin, delivery.SprintInput{ProjectID: project.ID, Name: "sprint", StartDate: "2026-09-01", EndDate: "2026-09-10"})
	if err != nil {
		t.Fatal(err)
	}
	milestone, err := operations.CreateMilestone(admin, delivery.MilestoneInput{ProjectID: project.ID, Name: "milestone", TargetDate: "2026-09-10"})
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := operations.Create(admin, delivery.CreateInput{Title: "dependency", Board: delivery.BoardResearchDelivery, ProjectID: project.ID, Owner: "old"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := operations.Create(admin, delivery.CreateInput{Title: "old", Board: delivery.BoardResearchDelivery, ProjectID: project.ID, Owner: "old", Priority: delivery.PriorityP1, ReleaseID: release.ID, SprintID: sprint.ID, MilestoneID: milestone.ID, StartDate: "2026-09-01", DueDate: "2026-09-02", EstimatePoints: 5, ProgressPercent: 50, Dependencies: []delivery.WorkItemDependency{{ItemID: dependency.ID, Relation: delivery.DependencyDependsOn}}, IoTBindings: []delivery.IoTBinding{{Kind: delivery.IoTBindingDevice, Reference: "x"}}, TraceLinks: []delivery.TraceLink{{Kind: delivery.TraceBuild, Reference: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	keepTitle := "kept"
	kept, err := operations.UpdateWorkItem(admin, item.ID, currentRevision(t, operations, admin, item.ID), delivery.WorkItemUpdate{Title: &keepTitle})
	if err != nil || kept.ReleaseID != release.ID || kept.SprintID != sprint.ID || kept.MilestoneID != milestone.ID || kept.StartDate != "2026-09-01" || kept.DueDate != "2026-09-02" || kept.EstimatePoints != 5 || kept.ProgressPercent != 50 || len(kept.Dependencies) != 1 || len(kept.IoTBindings) != 1 || len(kept.TraceLinks) != 1 {
		t.Fatalf("omitted fields not kept: %#v err=%v", kept, err)
	}
	title, owner, priority, empty := "new", "new", delivery.PriorityP0, ""
	zeroFloat := 0.0
	zeroInt := 0
	emptyDeps := []delivery.WorkItemDependency{}
	emptyBindings := []delivery.IoTBinding{}
	emptyTraces := []delivery.TraceLink{}
	updated, err := operations.UpdateWorkItem(admin, item.ID, currentRevision(t, operations, admin, item.ID), delivery.WorkItemUpdate{Title: &title, Owner: &owner, Priority: &priority, ReleaseID: &empty, SprintID: &empty, MilestoneID: &empty, StartDate: &empty, DueDate: &empty, EstimatePoints: &zeroFloat, ProgressPercent: &zeroInt, Dependencies: &emptyDeps, IoTBindings: &emptyBindings, TraceLinks: &emptyTraces})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "new" || updated.Owner != "new" || updated.Priority != delivery.PriorityP0 || updated.ReleaseID != "" || updated.SprintID != "" || updated.MilestoneID != "" || updated.StartDate != "" || updated.DueDate != "" || updated.EstimatePoints != 0 || updated.ProgressPercent != 0 || len(updated.Dependencies) != 0 || len(updated.IoTBindings) != 0 || len(updated.TraceLinks) != 0 {
		t.Fatalf("presence result=%#v", updated)
	}
	stored, err := repository.Get(ctx, item.ID)
	if err != nil || stored.Title != updated.Title || stored.Owner != updated.Owner || stored.Priority != updated.Priority || stored.ReleaseID != "" || stored.SprintID != "" || stored.MilestoneID != "" || stored.StartDate != "" || stored.DueDate != "" || stored.EstimatePoints != 0 || stored.ProgressPercent != 0 || len(stored.Dependencies) != 0 || len(stored.IoTBindings) != 0 || len(stored.TraceLinks) != 0 {
		t.Fatalf("stored=%#v err=%v", stored, err)
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

func TestOperationsPlanningCreatesUseGeneratedContractsAndPersistResponses(t *testing.T) {
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
	administrator := identity.WithPrincipal(ctx, identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		TenantID:      localauth.DevelopmentTenantID,
		Roles:         []string{localauth.RoleLocalAdmin},
	})

	project, err := operations.CreateProject(administrator, delivery.ProjectInput{Name: "Yunka 合同收口", Board: delivery.BoardProductPlatform, Owner: "产品负责人", Description: "生成合同不应丢失项目描述"})
	if err != nil {
		t.Fatalf("create project through generated operation: %v", err)
	}
	if project.ID == "" || project.Name != "Yunka 合同收口" || project.Board != delivery.BoardProductPlatform || project.Owner != "产品负责人" || project.Description != "生成合同不应丢失项目描述" {
		t.Fatalf("project response = %#v", project)
	}
	persistedProject, err := repository.GetProject(ctx, project.ID)
	if err != nil || persistedProject != project {
		t.Fatalf("persisted project = %#v, %v; want %#v", persistedProject, err, project)
	}
	projects, err := operations.ListProjects(administrator)
	if err != nil {
		t.Fatalf("list projects through generated operation: %v", err)
	}
	if len(projects) != 1 || projects[0] != project {
		t.Fatalf("listed projects = %#v, want %#v", projects, []delivery.Project{project})
	}

	release, err := operations.CreateRelease(administrator, delivery.ReleaseInput{ProjectID: project.ID, Name: "MVP", Version: "v0.1.0", TargetDate: "2026-09-30", Status: "planned", Description: "生成合同验证"})
	if err != nil {
		t.Fatalf("create release through generated operation: %v", err)
	}
	if release.ID == "" || release.ProjectID != project.ID || release.Name != "MVP" || release.Version != "v0.1.0" || release.TargetDate != "2026-09-30" || release.Status != "planned" || release.Description != "生成合同验证" {
		t.Fatalf("release response = %#v", release)
	}
	persistedRelease, err := repository.GetRelease(ctx, release.ID)
	if err != nil || persistedRelease != release {
		t.Fatalf("persisted release = %#v, %v; want %#v", persistedRelease, err, release)
	}
	releases, err := operations.ListReleases(administrator, project.ID)
	if err != nil {
		t.Fatalf("list releases through generated operation: %v", err)
	}
	if len(releases) != 1 || releases[0] != release {
		t.Fatalf("listed releases = %#v, want %#v", releases, []delivery.Release{release})
	}

	sprint, err := operations.CreateSprint(administrator, delivery.SprintInput{ProjectID: project.ID, Name: "Sprint 1", Goal: "合同收口", StartDate: "2026-09-03", EndDate: "2026-09-10", Status: "active"})
	if err != nil {
		t.Fatalf("create sprint through generated operation: %v", err)
	}
	if sprint.ID == "" || sprint.ProjectID != project.ID || sprint.Name != "Sprint 1" || sprint.Goal != "合同收口" || sprint.StartDate != "2026-09-03" || sprint.EndDate != "2026-09-10" || sprint.Status != "active" {
		t.Fatalf("sprint response = %#v", sprint)
	}
	persistedSprint, err := repository.GetSprint(ctx, sprint.ID)
	if err != nil || persistedSprint != sprint {
		t.Fatalf("persisted sprint = %#v, %v; want %#v", persistedSprint, err, sprint)
	}
	sprints, err := operations.ListSprints(administrator, project.ID)
	if err != nil {
		t.Fatalf("list sprints through generated operation: %v", err)
	}
	if len(sprints) != 1 || sprints[0] != sprint {
		t.Fatalf("listed sprints = %#v, want %#v", sprints, []delivery.Sprint{sprint})
	}

	milestone, err := operations.CreateMilestone(administrator, delivery.MilestoneInput{ProjectID: project.ID, Name: "合同完成", TargetDate: "2026-09-15", Status: "planned", Description: "发布前检查"})
	if err != nil {
		t.Fatalf("create milestone through generated operation: %v", err)
	}
	if milestone.ID == "" || milestone.ProjectID != project.ID || milestone.Name != "合同完成" || milestone.TargetDate != "2026-09-15" || milestone.Status != "planned" || milestone.Description != "发布前检查" {
		t.Fatalf("milestone response = %#v", milestone)
	}
	persistedMilestone, err := repository.GetMilestone(ctx, milestone.ID)
	if err != nil || persistedMilestone != milestone {
		t.Fatalf("persisted milestone = %#v, %v; want %#v", persistedMilestone, err, milestone)
	}
	milestones, err := operations.ListMilestones(administrator, project.ID)
	if err != nil {
		t.Fatalf("list milestones through generated operation: %v", err)
	}
	if len(milestones) != 1 || milestones[0] != milestone {
		t.Fatalf("listed milestones = %#v, want %#v", milestones, []delivery.Milestone{milestone})
	}

	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read planning-create outbox: %v", err)
	}
	if snapshot.Pending != 4 {
		t.Fatalf("planning creates queued %d events, want 4", snapshot.Pending)
	}
}

func TestOperationsPlanningCreatesRequireGeneratedWritePermission(t *testing.T) {
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
	viewer := identity.WithPrincipal(ctx, identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		Roles:         []string{localauth.RoleViewer},
	})

	for _, testCase := range []struct {
		name string
		call func() error
	}{
		{name: "project", call: func() error {
			_, err := operations.CreateProject(viewer, delivery.ProjectInput{Name: "denied", Board: delivery.BoardProductPlatform, Owner: "viewer"})
			return err
		}},
		{name: "release", call: func() error {
			_, err := operations.CreateRelease(viewer, delivery.ReleaseInput{ProjectID: "project-denied", Name: "denied"})
			return err
		}},
		{name: "sprint", call: func() error {
			_, err := operations.CreateSprint(viewer, delivery.SprintInput{ProjectID: "project-denied", Name: "denied"})
			return err
		}},
		{name: "milestone", call: func() error {
			_, err := operations.CreateMilestone(viewer, delivery.MilestoneInput{ProjectID: "project-denied", Name: "denied"})
			return err
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.call(); !authz.IsDenied(err) {
				t.Fatalf("viewer %s create error = %v, want authorization denial", testCase.name, err)
			}
		})
	}

	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read denied planning-create outbox: %v", err)
	}
	if snapshot.Pending != 0 {
		t.Fatalf("denied planning creates queued %d events, want 0", snapshot.Pending)
	}
}

func currentRevision(t *testing.T, operations *application.Operations, ctx context.Context, id string) int64 {
	t.Helper()
	items, err := operations.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == id {
			return item.Revision
		}
	}
	t.Fatalf("delivery item %q not found", id)
	return 0
}
