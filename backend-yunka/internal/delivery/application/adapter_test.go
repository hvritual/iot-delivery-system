package application_test

import (
	"context"
	"strings"
	"testing"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
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
	_, err = adapter.UpdateItem(context.Background(), &deliveryv1.UpdateItemRequest{Id: created.GetItem().GetId(), UpdateMask: []string{"unknown"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported delivery work item update field: unknown") {
		t.Fatalf("unknown mask error=%v", err)
	}
	stored, err := service.Get(context.Background(), created.GetItem().GetId())
	if err != nil || stored.Title != "item" {
		t.Fatalf("unknown mask changed item=%#v err=%v", stored, err)
	}
}
