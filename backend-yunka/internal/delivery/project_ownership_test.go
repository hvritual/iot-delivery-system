package delivery

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteRepositoryRoundTripsProjectOrganizationID(t *testing.T) {
	repository, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	project := Project{ID: "project-a", OrganizationID: "org-a", Name: "Project A", Board: BoardResearchDelivery, Owner: "owner", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := repository.CreateProject(t.Context(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	stored, err := repository.GetProject(t.Context(), project.ID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	if stored.OrganizationID != "org-a" {
		t.Fatalf("stored organization ID = %q, want org-a", stored.OrganizationID)
	}
}
