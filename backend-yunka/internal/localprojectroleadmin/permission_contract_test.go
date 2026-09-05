package localprojectroleadmin

import (
	"errors"
	"testing"
)

func TestYU24ProjectRoleAssignmentFailsClosedOnPermissionStatusDrift(t *testing.T) {
	fixture := newRoleFixture(t)
	if _, err := fixture.repository.Database().Exec(`UPDATE permissions SET status = 'reserved' WHERE id = 'delivery.projects.read'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "viewer"}); !errors.Is(err, ErrRoleContractDrift) {
		t.Fatalf("permission status drift assignment error=%v", err)
	}
	assertNoActiveTuple(t, fixture, "project-a", "target-a", "viewer")
}

func TestYU24ProjectRoleAssignmentFailsClosedOnPermissionScopeDrift(t *testing.T) {
	fixture := newRoleFixture(t)
	if _, err := fixture.repository.Database().Exec(`INSERT INTO permission_allowed_scopes (permission_id, scope_type) VALUES ('delivery.projects.read', 'organization')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "viewer"}); !errors.Is(err, ErrRoleContractDrift) {
		t.Fatalf("permission scope drift assignment error=%v", err)
	}
	assertNoActiveTuple(t, fixture, "project-a", "target-a", "viewer")
}
