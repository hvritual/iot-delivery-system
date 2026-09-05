package localprojectroleadmin

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestYU24ConcurrentDuplicateAssignmentsHaveOneWinner(t *testing.T) {
	fixture := newRoleFixture(t)
	var ids atomic.Int64
	fixture.manager.newID = func() (string, error) {
		return fmt.Sprintf("yu24-concurrent-%04d", ids.Add(1)), nil
	}
	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "viewer"})
			errorsCh <- err
		}()
	}
	close(start)
	successes, duplicates := 0, 0
	for range 2 {
		err := <-errorsCh
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrBindingAlreadyActive):
			duplicates++
		default:
			t.Fatalf("concurrent assignment error=%v", err)
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatalf("concurrent assignment successes=%d duplicates=%d", successes, duplicates)
	}
	var active int
	if err := fixture.repository.Database().QueryRow(`SELECT COUNT(*) FROM role_bindings WHERE organization_id = 'org-a' AND scope_type = 'project' AND scope_id = 'project-a' AND user_id = 'target-a' AND role_id = 'viewer' AND status = 'active'`).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active concurrent bindings=%d error=%v", active, err)
	}
}

func TestYU24ConcurrentRevocationsHaveOneCASWinner(t *testing.T) {
	fixture := newRoleFixture(t)
	assigned, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := fixture.manager.Revoke(fixture.adminCtx, RevokeInput{BindingID: assigned.BindingID, ExpectedRevision: 1})
			errorsCh <- err
		}()
	}
	close(start)
	successes, revoked := 0, 0
	for range 2 {
		err := <-errorsCh
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrBindingRevoked), errors.Is(err, ErrBindingRevisionConflict):
			revoked++
		default:
			t.Fatalf("concurrent revoke error=%v", err)
		}
	}
	if successes != 1 || revoked != 1 {
		t.Fatalf("concurrent revoke successes=%d stale=%d", successes, revoked)
	}
	var status string
	var revision int64
	if err := fixture.repository.Database().QueryRow(`SELECT status, revision FROM role_bindings WHERE id = ?`, assigned.BindingID).Scan(&status, &revision); err != nil || status != "disabled" || revision != 2 {
		t.Fatalf("concurrent revoke state=%s revision=%d error=%v", status, revision, err)
	}
}
