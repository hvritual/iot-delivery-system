package delivery

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"yunka.io/framework/core/identity"
)

func TestServiceRejectsImplementerProductionValidationWithoutSideEffects(t *testing.T) {
	service := NewService(NewMemoryRepository(), nil)
	implementer := humanPrincipalContext(t, "implementer")
	item, err := service.Create(t.Context(), CreateInput{
		Title: "segregated production validation",
		Board: BoardResearchDelivery,
		Owner: "a display-only owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []Gate{GateSolutionReviewed, GateDevelopmentCompleted, GateTestPassed} {
		item, err = service.AdvanceGate(implementer, item.ID, item.Revision, gate, []Evidence{{Kind: "test", Title: string(gate)}})
		if err != nil {
			t.Fatalf("advance to %s: %v", gate, err)
		}
	}
	before, err := service.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.AdvanceGate(implementer, item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "same person"}}); err == nil {
		t.Fatal("implementer production validation was accepted")
	}
	after, err := service.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected production validation changed item: before=%#v after=%#v", before, after)
	}
}

func TestServiceRejectsMalformedProductionPrincipalWithoutSideEffects(t *testing.T) {
	service := NewService(NewMemoryRepository(), nil)
	implementer := humanPrincipalContext(t, "implementer")
	item := advanceToTestPassed(t, service, implementer, "malformed reviewer")
	before, err := service.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	malformedReviewer := identity.WithPrincipal(t.Context(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodJWT,
		TenantID:      "org-a",
		UserID:        " reviewer ",
	})

	if _, err := service.AdvanceGate(malformedReviewer, item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "malformed"}}); err == nil {
		t.Fatal("malformed production principal was accepted")
	}
	after, err := service.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("malformed production principal changed item: before=%#v after=%#v", before, after)
	}
}

func TestTrustedPrincipalSourceRejectsUnsupportedOrInconsistentIdentity(t *testing.T) {
	for name, principal := range map[string]identity.Principal{
		"unknown authentication method": {Authenticated: true, AuthMethod: "legacy", TenantID: "org-a", UserID: "legacy-user"},
		"JWT without user ID":           {Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", Subject: "subject-user"},
		"service token with user ID":    {Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "service-account/release", UserID: "user-a"},
		"service token without account": {Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "release"},
		"API key without user ID":       {Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "org-a", Subject: "local-api-key/admin"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := trustedPrincipalSource(identity.WithPrincipal(t.Context(), principal)); ok {
				t.Fatalf("principal %#v was accepted", principal)
			}
		})
	}
}

func TestServiceRejectsMalformedPersistedImplementationSourceWithoutSideEffects(t *testing.T) {
	for name, source := range map[string]PrincipalSource{
		"partial":         {Kind: "human", AuthMethod: identity.AuthMethodJWT, SubjectID: "implementer"},
		"unknown method":  {Kind: "other", AuthMethod: "legacy", SubjectID: "implementer", TenantID: "org-a"},
		"kind mismatch":   {Kind: "service", AuthMethod: identity.AuthMethodJWT, SubjectID: "implementer", TenantID: "org-a"},
		"invalid service": {Kind: "service", AuthMethod: identity.AuthMethodServiceToken, SubjectID: "implementer", TenantID: "org-a"},
		"invalid API key": {Kind: "development-api-key", AuthMethod: identity.AuthMethodAPIKey, SubjectID: " local ", TenantID: "org-a"},
	} {
		t.Run(name, func(t *testing.T) {
			service := NewService(NewMemoryRepository(), nil)
			item := advanceToTestPassed(t, service, humanPrincipalContext(t, "implementer"), "malformed source "+name)
			item.ImplementationPrincipal = source
			if err := saveWorkItemForTest(t.Context(), service.repository, item); err != nil {
				t.Fatal(err)
			}
			before, err := service.Get(t.Context(), item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.AdvanceGate(humanPrincipalContext(t, "reviewer"), item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: name}}); !errors.Is(err, ErrImplementationSourceRequired) {
				t.Fatalf("production validation error = %v", err)
			}
			after, err := service.Get(t.Context(), item.ID)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("malformed source changed item: %#v, %v", after, err)
			}
		})
	}
}

func TestServiceRejectsMalformedPersistedImplementationSourceWhenClosing(t *testing.T) {
	service := NewService(NewMemoryRepository(), nil)
	implementer := humanPrincipalContext(t, "implementer")
	item := advanceToTestPassed(t, service, implementer, "malformed close source")
	reviewer := humanPrincipalContext(t, "reviewer")
	if _, err := service.AdvanceGate(reviewer, item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "independent"}}); err != nil {
		t.Fatal(err)
	}
	stored, err := service.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.ImplementationPrincipal = PrincipalSource{Kind: "human", AuthMethod: identity.AuthMethodJWT, SubjectID: "implementer"}
	if err := saveWorkItemForTest(t.Context(), service.repository, stored); err != nil {
		t.Fatal(err)
	}
	before, err := service.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Close(reviewer, item.ID, serviceRevision(t, service, item.ID), "retrospective"); !errors.Is(err, ErrImplementationSourceRequired) {
		t.Fatalf("malformed-source close error = %v", err)
	}
	after, err := service.Get(t.Context(), item.ID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("malformed-source close changed item: %#v, %v", after, err)
	}
}

func TestServiceRejectsCrossTenantProductionValidationAndCloseWithoutSideEffects(t *testing.T) {
	t.Run("production validation", func(t *testing.T) {
		service := NewService(NewMemoryRepository(), nil)
		item := advanceToTestPassed(t, service, humanPrincipalContextForTenant(t, "org-a", "implementer"), "cross tenant validation")
		before, err := service.Get(t.Context(), item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.AdvanceGate(humanPrincipalContextForTenant(t, "org-b", "reviewer"), item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "cross tenant"}}); !errors.Is(err, ErrImplementationSourceRequired) {
			t.Fatalf("cross-tenant production validation error = %v", err)
		}
		after, err := service.Get(t.Context(), item.ID)
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatalf("cross-tenant production validation changed item: %#v, %v", after, err)
		}
	})

	t.Run("close", func(t *testing.T) {
		service := NewService(NewMemoryRepository(), nil)
		implementer := humanPrincipalContextForTenant(t, "org-a", "implementer")
		item := advanceToTestPassed(t, service, implementer, "cross tenant close")
		if _, err := service.AdvanceGate(humanPrincipalContextForTenant(t, "org-a", "reviewer"), item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "same tenant"}}); err != nil {
			t.Fatal(err)
		}
		before, err := service.Get(t.Context(), item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Close(humanPrincipalContextForTenant(t, "org-b", "reviewer"), item.ID, serviceRevision(t, service, item.ID), "cross tenant retrospective"); !errors.Is(err, ErrImplementationSourceRequired) {
			t.Fatalf("cross-tenant close error = %v", err)
		}
		after, err := service.Get(t.Context(), item.ID)
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatalf("cross-tenant close changed item: %#v, %v", after, err)
		}
	})
}

func TestServiceAllowsDifferentJWTReviewerAndClosesWithoutThreePersonRule(t *testing.T) {
	for _, owner := range []string{"implementer", "reviewer"} {
		t.Run(owner, func(t *testing.T) {
			service := NewService(NewMemoryRepository(), nil)
			implementer := humanPrincipalContext(t, "implementer")
			reviewer := humanPrincipalContext(t, "reviewer")
			item := advanceToTestPassed(t, service, implementer, "different reviewer "+owner)
			item.Owner = owner
			if err := saveWorkItemForTest(t.Context(), service.repository, item); err != nil {
				t.Fatal(err)
			}

			validated, err := service.AdvanceGate(reviewer, item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "independent review"}})
			if err != nil {
				t.Fatalf("different reviewer production validation: %v", err)
			}
			if validated.ImplementationPrincipal != (PrincipalSource{Kind: "human", AuthMethod: identity.AuthMethodJWT, SubjectID: "implementer", TenantID: "org-a"}) {
				t.Fatalf("implementation principal = %#v", validated.ImplementationPrincipal)
			}
			if validated.ProductionValidationPrincipal != (PrincipalSource{Kind: "human", AuthMethod: identity.AuthMethodJWT, SubjectID: "reviewer", TenantID: "org-a"}) {
				t.Fatalf("validation principal = %#v", validated.ProductionValidationPrincipal)
			}

			beforeClose, err := service.Get(t.Context(), item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Close(implementer, item.ID, serviceRevision(t, service, item.ID), "independent review completed"); !errors.Is(err, ErrImplementerCannotVerifyOwnChange) {
				t.Fatalf("implementer close error = %v", err)
			}
			afterRejectedClose, err := service.Get(t.Context(), item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(afterRejectedClose, beforeClose) {
				t.Fatal("rejected close changed item")
			}
			closed, err := service.Close(reviewer, item.ID, serviceRevision(t, service, item.ID), "independent review completed")
			if err != nil {
				t.Fatalf("reviewer close: %v", err)
			}
			if closed.Status != StatusClosed {
				t.Fatalf("close status = %q", closed.Status)
			}
		})
	}
}

func TestServiceFailsClosedForNonHumanAndLegacyProductionActions(t *testing.T) {
	service := NewService(NewMemoryRepository(), nil)
	implementer := humanPrincipalContext(t, "implementer")
	item := advanceToTestPassed(t, service, implementer, "nonhuman validation")
	apiKey := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "org-a", UserID: "local-admin"})
	serviceIdentity := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "service-account/release"})
	for name, actor := range map[string]context.Context{"missing": t.Context(), "api-key": apiKey, "service": serviceIdentity} {
		t.Run(name, func(t *testing.T) {
			before, err := service.Get(t.Context(), item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.AdvanceGate(actor, item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: name}}); !errors.Is(err, ErrProductionPrincipalRequired) {
				t.Fatalf("production validation error = %v", err)
			}
			after, err := service.Get(t.Context(), item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatal("rejected production validation changed item")
			}
		})
	}

	legacy := item
	legacy.ImplementationPrincipal = PrincipalSource{}
	if err := saveWorkItemForTest(t.Context(), service.repository, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdvanceGate(humanPrincipalContext(t, "reviewer"), legacy.ID, serviceRevision(t, service, legacy.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "legacy"}}); !errors.Is(err, ErrImplementationSourceRequired) {
		t.Fatalf("legacy production validation error = %v", err)
	}

	closable := advanceToTestPassed(t, service, implementer, "nonhuman close")
	if _, err := service.AdvanceGate(humanPrincipalContext(t, "reviewer"), closable.ID, serviceRevision(t, service, closable.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "independent"}}); err != nil {
		t.Fatal(err)
	}
	for name, actor := range map[string]context.Context{"missing": t.Context(), "api-key": apiKey, "service": serviceIdentity} {
		t.Run("close-"+name, func(t *testing.T) {
			before, err := service.Get(t.Context(), closable.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Close(actor, closable.ID, serviceRevision(t, service, closable.ID), "retrospective"); !errors.Is(err, ErrProductionPrincipalRequired) {
				t.Fatalf("close error = %v", err)
			}
			after, err := service.Get(t.Context(), closable.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatal("rejected close changed item")
			}
		})
	}
	legacyClose, err := service.Get(t.Context(), closable.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacyClose.ImplementationPrincipal = PrincipalSource{}
	if err := saveWorkItemForTest(t.Context(), service.repository, legacyClose); err != nil {
		t.Fatal(err)
	}
	beforeLegacyClose, err := service.Get(t.Context(), legacyClose.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Close(humanPrincipalContext(t, "reviewer"), legacyClose.ID, serviceRevision(t, service, legacyClose.ID), "retrospective"); !errors.Is(err, ErrImplementationSourceRequired) {
		t.Fatalf("legacy close error = %v", err)
	}
	afterLegacyClose, err := service.Get(t.Context(), legacyClose.ID)
	if err != nil || !reflect.DeepEqual(afterLegacyClose, beforeLegacyClose) {
		t.Fatalf("legacy close changed item: %#v, %v", afterLegacyClose, err)
	}
}

func saveWorkItemForTest(ctx context.Context, repository Repository, item WorkItem) error {
	expectedRevision := item.Revision
	item.Revision++
	return repository.Save(ctx, item, expectedRevision)
}

func serviceRevision(t *testing.T, service *Service, id string) int64 {
	t.Helper()
	item, err := service.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return item.Revision
}

func TestSQLiteRepositoryRetainsImplementationSourceForSeparationOfDuties(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "delivery.db")
	repository, err := NewSQLiteRepository(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	implementer := humanPrincipalContext(t, "implementer")
	item := advanceToTestPassed(t, NewService(repository, nil), implementer, "sqlite source")
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteRepository(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	service := NewService(reopened, nil)
	if _, err := service.AdvanceGate(implementer, item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "same after reopen"}}); !errors.Is(err, ErrImplementerCannotVerifyOwnChange) {
		t.Fatalf("same implementer after reopen error = %v", err)
	}
	if _, err := service.AdvanceGate(humanPrincipalContext(t, "reviewer"), item.ID, serviceRevision(t, service, item.ID), GateProductionValidated, []Evidence{{Kind: "validation", Title: "different after reopen"}}); err != nil {
		t.Fatalf("different reviewer after reopen: %v", err)
	}
}

func advanceToTestPassed(t *testing.T, service *Service, implementer context.Context, title string) WorkItem {
	t.Helper()
	item, err := service.Create(t.Context(), CreateInput{Title: title, Board: BoardResearchDelivery, Owner: "not an identity"})
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []Gate{GateSolutionReviewed, GateDevelopmentCompleted, GateTestPassed} {
		item, err = service.AdvanceGate(implementer, item.ID, item.Revision, gate, []Evidence{{Kind: "test", Title: string(gate)}})
		if err != nil {
			t.Fatalf("advance to %s: %v", gate, err)
		}
	}
	return item
}

func humanPrincipalContext(t *testing.T, userID string) context.Context {
	return humanPrincipalContextForTenant(t, "org-a", userID)
}

func humanPrincipalContextForTenant(t *testing.T, tenantID, userID string) context.Context {
	t.Helper()
	return identity.WithPrincipal(t.Context(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodJWT,
		Subject:       "subject-" + userID,
		TenantID:      tenantID,
		UserID:        userID,
	})
}
