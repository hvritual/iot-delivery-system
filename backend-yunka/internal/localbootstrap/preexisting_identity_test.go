package localbootstrap

import (
	"errors"
	"testing"
)

func TestYU19IdentityCreatedAfterMigrationPermanentlyClosesBootstrap(t *testing.T) {
	fixture := newFixture(t)
	if _, err := fixture.database.Exec(`INSERT INTO users (id, organization_id, display_name) VALUES ('late-user', 'org-a', 'Late User')`); err != nil {
		t.Fatal(err)
	}
	input := InitializeInput{OrganizationID: "org-a", DisplayName: "Anonymous Escalation", Password: []byte("must-not-create")}
	if _, err := fixture.manager.Initialize(t.Context(), input); !errors.Is(err, ErrPreexistingIdentity) {
		t.Fatalf("late identity bootstrap error=%v, want ErrPreexistingIdentity", err)
	}
	var reason string
	if err := fixture.database.QueryRow(`SELECT close_reason FROM iotd_local_admin_bootstrap_state WHERE id = ?`, stateID).Scan(&reason); err != nil || reason != "preexisting_identity" {
		t.Fatalf("late identity close reason=%q err=%v", reason, err)
	}
	if _, err := fixture.database.Exec(`DELETE FROM users WHERE id = 'late-user'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Initialize(t.Context(), input); !errors.Is(err, ErrPreexistingIdentity) {
		t.Fatalf("deleted late identity reopened bootstrap: %v", err)
	}
	assertCounts(t, fixture.database, 0, 0, 0, 1, 0)
}
