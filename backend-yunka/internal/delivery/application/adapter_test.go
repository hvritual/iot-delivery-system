package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestAdapterImplementsGeneratedPortAndMapsCreateItem(t *testing.T) {
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	adapter := application.NewAdapter(service)
	var _ application.DeliveryService = adapter

	response, err := adapter.CreateItem(context.Background(), &deliveryv1.CreateItemRequest{
		Title:    "生成应用端口验收",
		Board:    string(delivery.BoardResearchDelivery),
		Owner:    "研发负责人",
		Priority: string(delivery.PriorityP0),
	})
	if err != nil {
		t.Fatalf("create through generated application port: %v", err)
	}
	if response.GetItem().GetTitle() != "生成应用端口验收" || response.GetItem().GetStatus() != string(delivery.StatusPlanned) {
		t.Fatalf("response item = %#v, want created planned delivery item", response.GetItem())
	}
}

func TestAdapterRejectsUnknownUpdateMask(t *testing.T) {
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	adapter := application.NewAdapter(service)
	created, err := adapter.CreateItem(context.Background(), &deliveryv1.CreateItemRequest{Title: "item", Board: string(delivery.BoardResearchDelivery), Owner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.UpdateItem(context.Background(), &deliveryv1.UpdateItemRequest{Id: created.GetItem().GetId(), ExpectedRevision: created.GetItem().GetRevision(), UpdateMask: []string{"unknown"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported delivery work item update field: unknown") {
		t.Fatalf("unknown mask error=%v", err)
	}
	stored, err := service.Get(context.Background(), created.GetItem().GetId())
	if err != nil || stored.Title != "item" {
		t.Fatalf("unknown mask changed item=%#v err=%v", stored, err)
	}
}

func TestAdapterClassifiesSelfProductionVerificationAsPermissionDenied(t *testing.T) {
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	adapter := application.NewAdapter(service)
	implementer := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "implementer"})

	created, err := adapter.CreateItem(implementer, &deliveryv1.CreateItemRequest{Title: "segregated", Board: string(delivery.BoardResearchDelivery), Owner: "owner"})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	for _, gate := range []delivery.Gate{delivery.GateSolutionReviewed, delivery.GateDevelopmentCompleted, delivery.GateTestPassed} {
		current, getErr := service.Get(t.Context(), created.GetItem().GetId())
		if getErr != nil {
			t.Fatal(getErr)
		}
		if _, err := adapter.AdvanceGate(implementer, &deliveryv1.AdvanceGateRequest{Id: created.GetItem().GetId(), ExpectedRevision: current.Revision, Gate: string(gate), Evidence: []*deliveryv1.Evidence{{Kind: "test", Title: string(gate)}}}); err != nil {
			t.Fatalf("advance %s: %v", gate, err)
		}
	}

	current, getErr := service.Get(t.Context(), created.GetItem().GetId())
	if getErr != nil {
		t.Fatal(getErr)
	}
	_, err = adapter.AdvanceGate(implementer, &deliveryv1.AdvanceGateRequest{Id: created.GetItem().GetId(), ExpectedRevision: current.Revision, Gate: string(delivery.GateProductionValidated), Evidence: []*deliveryv1.Evidence{{Kind: "validation", Title: "self review"}}})
	if !authz.IsDenied(err) || !errors.Is(err, delivery.ErrImplementerCannotVerifyOwnChange) {
		t.Fatalf("self production verification error = %v, want permission-denied wrapper preserving the domain sentinel", err)
	}
}
