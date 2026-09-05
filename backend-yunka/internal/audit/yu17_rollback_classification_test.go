package audit

import "testing"

func TestYU17RollbackAuditClassificationFollowsCanonicalOperationDomain(t *testing.T) {
	for _, test := range []struct {
		operation string
		category  EventCategory
		reason    string
	}{
		{
			operation: "config.revisions.change",
			category:  EventCategoryConfiguration,
			reason:    "configuration.transaction_rolled_back",
		},
		{
			operation: "config.revisions.rollback",
			category:  EventCategoryConfiguration,
			reason:    "configuration.transaction_rolled_back",
		},
		{
			operation: "config.revisions.unregistered",
			category:  EventCategoryDelivery,
			reason:    "application.transaction_rolled_back",
		},
		{
			operation: "delivery.items.create",
			category:  EventCategoryDelivery,
			reason:    "application.transaction_rolled_back",
		},
	} {
		t.Run(test.operation, func(t *testing.T) {
			category, reason := rollbackAuditClassification(test.operation)
			if category != test.category || reason != test.reason {
				t.Fatalf("rollback classification for %q = %q/%q, want %q/%q", test.operation, category, reason, test.category, test.reason)
			}
		})
	}
}
