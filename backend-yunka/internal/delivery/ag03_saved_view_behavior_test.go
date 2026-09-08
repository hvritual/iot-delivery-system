package delivery

import (
	"context"
	"errors"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This characterization also runs unchanged against the pre-refactor Git base.
func TestAG03SavedViewBehaviorParity(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			var repo Repository
			if kind == "memory" {
				repo = NewMemoryRepository()
			} else {
				r, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "views.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = r.Close() })
				repo = r
			}
			now := time.Date(2026, 9, 8, 10, 11, 12, 123, time.FixedZone("local", 7200))
			service := NewService(repo, nil)
			service.now = func() time.Time { return now }
			ctx := identity.WithPrincipal(context.Background(), identity.Principal{Authenticated: true, UserID: " alice ", Subject: "display"})
			input := SavedViewInput{Name: "  my work  ", Filter: WorkItemFilter{ProjectID: " p ", Owner: " bob ", ReleaseID: " r ", SprintID: " s ", MilestoneID: " m ", Query: " q ", Board: Board(" enum "), Status: Status(" status "), Kind: WorkItemKind(" kind ")}}
			if _, err := service.SaveView(context.Background(), SavedViewInput{}); err == nil || err.Error() != "delivery saved view name is required" {
				t.Fatalf("name validation order: %v", err)
			}
			for _, principal := range []identity.Principal{{UserID: "alice"}, {Authenticated: true, Subject: "alice"}, {Authenticated: true, UserID: " "}} {
				bad := identity.WithPrincipal(context.Background(), principal)
				if _, err := service.SaveView(bad, input); !errors.Is(err, ErrCanonicalUserRequired) {
					t.Fatalf("invalid caller write: %v", err)
				}
				if _, err := service.ListSavedViews(bad); !errors.Is(err, ErrCanonicalUserRequired) {
					t.Fatalf("invalid caller read: %v", err)
				}
			}
			// Other delivery entities consume the same sequence. A different user's
			// persisted view must still participate in global collision detection.
			project, err := service.CreateProject(ctx, ProjectInput{Name: "P", Board: BoardOperations, Owner: "owner"})
			if err != nil || project.ID != "PRJ-20260908-0001" {
				t.Fatalf("project sequence: %+v %v", project, err)
			}
			existing := SavedView{ID: "VIEW-20260908-0002", Name: "other", Owner: "bob", CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
			if err := repo.CreateSavedView(ctx, existing); err != nil {
				t.Fatal(err)
			}
			view, err := service.SaveView(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			expected := SavedView{ID: "VIEW-20260908-0003", Name: "my work", Owner: "alice", Filter: WorkItemFilter{ProjectID: "p", Owner: "bob", ReleaseID: "r", SprintID: "s", MilestoneID: "m", Query: "q", Board: Board(" enum "), Status: Status(" status "), Kind: WorkItemKind(" kind ")}, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
			if !reflect.DeepEqual(view, expected) {
				t.Fatalf("got %#v want %#v", view, expected)
			}
			listed, err := service.ListSavedViews(ctx)
			if err != nil || !reflect.DeepEqual(listed, []SavedView{expected}) {
				t.Fatalf("owner list: %#v %v", listed, err)
			}
			all, err := repo.ListSavedViews(ctx, "")
			if err != nil || len(all) != 2 || all[0].ID != existing.ID {
				t.Fatalf("stable order: %#v %v", all, err)
			}
			other := identity.WithPrincipal(context.Background(), identity.Principal{Authenticated: true, UserID: "carol", Subject: "display"})
			listed, err = service.ListSavedViews(other)
			if err != nil || len(listed) != 0 {
				t.Fatalf("owner isolation: %#v %v", listed, err)
			}
			for _, broken := range []*Service{nil, NewService(nil, nil)} {
				if _, err := broken.SaveView(ctx, input); err == nil || err.Error() != "delivery service is not configured" {
					t.Fatalf("nil facade: %v", err)
				}
				if _, err := broken.ListSavedViews(ctx); err == nil {
					t.Fatal("nil read succeeded")
				}
			}
		})
	}
}

func TestAG03SavedViewSQLiteReopenAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.db")
	repo, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC)
	value := SavedView{ID: "saved", Name: "before restart", Owner: "owner", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSavedView(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	repo, err = NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	views, err := repo.ListSavedViews(context.Background(), "owner")
	if err != nil || !reflect.DeepEqual(views, []SavedView{value}) {
		t.Fatalf("reopen: %v %v", views, err)
	}
	if err := repo.CreateSavedView(context.Background(), value); err == nil || !strings.Contains(err.Error(), "insert delivery saved view:") {
		t.Fatalf("duplicate: %v", err)
	}
	_, err = repo.Database().Exec(`UPDATE iotd_delivery_saved_views SET payload = '{'`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ListSavedViews(context.Background(), "owner"); err == nil || !strings.HasPrefix(err.Error(), "decode delivery saved view:") {
		t.Fatalf("bad persisted JSON: %v", err)
	}
}
