package delivery

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMemoryRepositoryCompareAndSwapKeepsWinningPayload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repository := NewMemoryRepository()
	item := WorkItem{ID: "WI-memory-cas", Title: "before", Revision: 1, UpdatedAt: time.Now().UTC()}
	if err := repository.Create(ctx, item); err != nil {
		t.Fatalf("create item: %v", err)
	}

	winner := item
	winner.Title = "winner"
	winner.Revision = item.Revision + 1
	if err := repository.Save(ctx, winner, item.Revision); err != nil {
		t.Fatalf("save winner: %v", err)
	}

	loser := item
	loser.Title = "loser"
	loser.Revision = item.Revision + 1
	if err := repository.Save(ctx, loser, item.Revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale save error = %v, want ErrRevisionConflict", err)
	}

	stored, err := repository.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get stored item: %v", err)
	}
	if stored.Title != winner.Title || stored.Revision != 2 {
		t.Fatalf("stored item = %#v, want winning payload at revision 2", stored)
	}
}

func TestSQLiteRepositoryMigratesLegacyItemsAndUsesColumnRevision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	if _, err := legacy.Database().ExecContext(ctx, `DROP TABLE iotd_delivery_items; CREATE TABLE iotd_delivery_items (id TEXT PRIMARY KEY, payload TEXT NOT NULL, updated_at TEXT NOT NULL); INSERT INTO iotd_delivery_items (id, payload, updated_at) VALUES ('WI-legacy', '{"id":"WI-legacy","title":"legacy","revision":999}', '2026-09-04T00:00:00Z')`); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy repository: %v", err)
	}

	repository, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatalf("migrate repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })

	stored, err := repository.Get(ctx, "WI-legacy")
	if err != nil {
		t.Fatalf("read migrated item: %v", err)
	}
	if stored.Revision != 1 {
		t.Fatalf("migrated revision = %d, want 1", stored.Revision)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close migrated repository: %v", err)
	}
	repository, err = NewSQLiteRepository(path)
	if err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if _, err := repository.Database().ExecContext(ctx, `INSERT INTO iotd_delivery_items (id, payload, updated_at, revision) VALUES ('bad-revision', '{}', '2026-09-04T00:00:00Z', 0)`); err == nil {
		t.Fatal("revision constraint accepted zero")
	}
}

func TestSQLiteRepositoryRejectsWeakExistingRevisionSchema(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "weak.db")
	repository, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	if _, err := repository.Database().Exec(`DROP TABLE iotd_delivery_items; CREATE TABLE iotd_delivery_items (id TEXT PRIMARY KEY, payload TEXT NOT NULL, updated_at TEXT NOT NULL, revision INTEGER)`); err != nil {
		t.Fatalf("create weak schema: %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close weak schema repository: %v", err)
	}
	if _, err := NewSQLiteRepository(path); err == nil {
		t.Fatal("weak revision schema was accepted")
	}
}

func TestSQLiteRepositoryRejectsNonIntegerExistingRevisionSchema(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "non-integer-revision.db")
	repository, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	if _, err := repository.Database().Exec(`DROP TABLE iotd_delivery_items; CREATE TABLE iotd_delivery_items (id TEXT PRIMARY KEY, payload TEXT NOT NULL, updated_at TEXT NOT NULL, revision TEXT NOT NULL CHECK (revision > 0))`); err != nil {
		t.Fatalf("create non-integer revision schema: %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close non-integer revision schema repository: %v", err)
	}
	if _, err := NewSQLiteRepository(path); err == nil {
		t.Fatal("non-integer revision schema was accepted")
	}
}

func TestRepositoryClassifiesNotFoundAndRejectsNegativeExpectedRevision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	for name, open := range map[string]func(t *testing.T) Repository{
		"memory": func(_ *testing.T) Repository { return NewMemoryRepository() },
		"sqlite": func(t *testing.T) Repository {
			repository, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "classification.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = repository.Close() })
			return repository
		},
	} {
		t.Run(name, func(t *testing.T) {
			repository := open(t)
			candidate := WorkItem{ID: "missing", Revision: 2, UpdatedAt: time.Now().UTC()}
			if err := repository.Save(ctx, candidate, 1); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing save error = %v, want ErrNotFound", err)
			}
			if err := repository.Save(ctx, candidate, -1); !errors.Is(err, ErrInvalidExpectedRevision) {
				t.Fatalf("negative expected revision error = %v, want ErrInvalidExpectedRevision", err)
			}
		})
	}
}

func TestSQLiteRepositoryConcurrentCompareAndSwapHasOneWinner(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repository, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "cas.db"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	item := WorkItem{ID: "WI-sqlite-cas", Title: "before", Revision: 1, UpdatedAt: time.Now().UTC()}
	if err := repository.Create(ctx, item); err != nil {
		t.Fatalf("create item: %v", err)
	}

	start := make(chan struct{})
	errorsByTitle := make(chan struct {
		title string
		err   error
	}, 2)
	var group sync.WaitGroup
	for _, title := range []string{"writer-a", "writer-b"} {
		group.Go(func() {
			<-start
			candidate := item
			candidate.Title = title
			candidate.Revision = 2
			errorsByTitle <- struct {
				title string
				err   error
			}{title: title, err: repository.Save(ctx, candidate, item.Revision)}
		})
	}
	close(start)
	group.Wait()
	close(errorsByTitle)

	successes := 0
	conflicts := 0
	for result := range errorsByTitle {
		switch {
		case result.err == nil:
			successes++
		case errors.Is(result.err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("writer %q error = %v", result.title, result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("CAS outcomes = %d successes, %d conflicts; want one each", successes, conflicts)
	}
	stored, err := repository.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get winner: %v", err)
	}
	if stored.Revision != 2 || (stored.Title != "writer-a" && stored.Title != "writer-b") {
		t.Fatalf("stored winner = %#v, want exactly one complete winner at revision 2", stored)
	}
}

func TestServiceUpdateWorkItemRequiresCurrentExpectedRevision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service := NewService(NewMemoryRepository(), nil)
	created, err := service.Create(ctx, CreateInput{Title: "revisioned item", Board: BoardResearchDelivery, Owner: "owner"})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	title := "first update"
	if _, err := service.UpdateWorkItem(ctx, created.ID, 0, WorkItemUpdate{Title: &title}); !errors.Is(err, ErrInvalidExpectedRevision) {
		t.Fatalf("missing expected revision error = %v, want ErrInvalidExpectedRevision", err)
	}
	if _, err := service.UpdateWorkItem(ctx, created.ID, -1, WorkItemUpdate{Title: &title}); !errors.Is(err, ErrInvalidExpectedRevision) {
		t.Fatalf("negative expected revision error = %v, want ErrInvalidExpectedRevision", err)
	}
	unchanged, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get unchanged item: %v", err)
	}
	if unchanged.Revision != created.Revision || unchanged.Title != created.Title {
		t.Fatalf("missing expected revision changed item: %#v", unchanged)
	}

	updated, err := service.UpdateWorkItem(ctx, created.ID, created.Revision, WorkItemUpdate{Title: &title})
	if err != nil {
		t.Fatalf("update item: %v", err)
	}
	if updated.Revision != created.Revision+1 {
		t.Fatalf("updated revision = %d, want %d", updated.Revision, created.Revision+1)
	}
	staleTitle := "stale update"
	if _, err := service.UpdateWorkItem(ctx, created.ID, created.Revision, WorkItemUpdate{Title: &staleTitle}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update error = %v, want ErrRevisionConflict", err)
	}
	stored, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get stored item: %v", err)
	}
	if stored.Revision != updated.Revision || stored.Title != updated.Title {
		t.Fatalf("stale update changed item: %#v", stored)
	}
}

func TestServiceOtherMutationsRejectMissingExpectedRevisionWithoutChanges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service := NewService(NewMemoryRepository(), nil)
	created, err := service.Create(ctx, CreateInput{Title: "protected item", Board: BoardResearchDelivery, Owner: "owner"})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	for name, mutate := range map[string]func() error{
		"comment": func() error {
			_, err := service.AddComment(ctx, created.ID, 0, CommentInput{Body: "comment"})
			return err
		},
		"context": func() error {
			plan := "plan"
			_, err := service.UpdateContext(ctx, created.ID, 0, ContextUpdate{Plan: &plan})
			return err
		},
		"advance gate": func() error {
			_, err := service.AdvanceGate(ctx, created.ID, 0, GateSolutionReviewed, []Evidence{{Kind: "review", Title: "approved"}})
			return err
		},
		"close": func() error {
			_, err := service.Close(ctx, created.ID, 0, "retrospective")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(); !errors.Is(err, ErrInvalidExpectedRevision) {
				t.Fatalf("missing expected revision error = %v, want ErrInvalidExpectedRevision", err)
			}
			if err := mutateNegativeExpectedRevision(service, ctx, created.ID, name); !errors.Is(err, ErrInvalidExpectedRevision) {
				t.Fatalf("negative expected revision error = %v, want ErrInvalidExpectedRevision", err)
			}
			stored, err := service.Get(ctx, created.ID)
			if err != nil {
				t.Fatalf("get unchanged item: %v", err)
			}
			if stored.Revision != created.Revision || stored.Title != created.Title || len(stored.Comments) != 0 {
				t.Fatalf("missing expected revision changed item: %#v", stored)
			}
		})
	}
}

func mutateNegativeExpectedRevision(service *Service, ctx context.Context, id, mutation string) error {
	switch mutation {
	case "comment":
		_, err := service.AddComment(ctx, id, -1, CommentInput{Body: "comment"})
		return err
	case "context":
		plan := "plan"
		_, err := service.UpdateContext(ctx, id, -1, ContextUpdate{Plan: &plan})
		return err
	case "advance gate":
		_, err := service.AdvanceGate(ctx, id, -1, GateSolutionReviewed, []Evidence{{Kind: "review", Title: "approved"}})
		return err
	case "close":
		_, err := service.Close(ctx, id, -1, "retrospective")
		return err
	default:
		return errors.New("unknown test mutation")
	}
}
