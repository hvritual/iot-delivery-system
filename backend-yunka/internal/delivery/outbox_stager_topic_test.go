package delivery

import "testing"

func TestYU15MutationEventTopicsFollowAggregateBoundary(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		eventType string
		wantTopic string
	}{
		{eventType: "delivery.work-item.created", wantTopic: "delivery.work-item"},
		{eventType: "delivery.project.created", wantTopic: "delivery.project"},
		{eventType: "delivery.release.created", wantTopic: "delivery.release"},
		{eventType: "delivery.sprint.created", wantTopic: "delivery.sprint"},
		{eventType: "delivery.milestone.created", wantTopic: "delivery.milestone"},
		{eventType: "delivery.saved-view.saved", wantTopic: "delivery.saved-view"},
	} {
		t.Run(test.eventType, func(t *testing.T) {
			topic, err := mutationEventTopic(test.eventType)
			if err != nil {
				t.Fatalf("mutation event topic: %v", err)
			}
			if topic != test.wantTopic {
				t.Fatalf("topic = %q, want %q", topic, test.wantTopic)
			}
		})
	}
}

func TestYU15MutationEventTopicRejectsUnscopedEvent(t *testing.T) {
	t.Parallel()
	if _, err := mutationEventTopic("delivery.created"); err == nil {
		t.Fatal("unscoped delivery event type was accepted")
	}
}
