package audit

import "testing"

func TestYU20MemberAdminRollbackClassificationUsesIdentityFailureReason(t *testing.T) {
	for _, operationID := range []string{
		"identity.members.create",
		"identity.members.disable",
		"identity.members.credentials.reset",
	} {
		category, reason := rollbackAuditClassification(operationID)
		if category != EventCategoryConfiguration || reason != "identity.transaction_rolled_back" {
			t.Fatalf("rollback classification for %q = %q/%q", operationID, category, reason)
		}
	}
	category, reason := rollbackAuditClassification("identity.members.unregistered")
	if category != EventCategoryDelivery || reason != "application.transaction_rolled_back" {
		t.Fatalf("unregistered identity operation received special rollback classification: %q/%q", category, reason)
	}
}
