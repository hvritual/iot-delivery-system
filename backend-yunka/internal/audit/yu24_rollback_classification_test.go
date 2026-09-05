package audit

import "testing"

func TestYU24ProjectRoleRollbackUsesIdentityClassification(t *testing.T) {
	for _, operation := range []string{
		"identity.project-role-bindings.assign",
		"identity.project-role-bindings.revoke",
	} {
		category, reason := rollbackAuditClassification(operation)
		if category != EventCategoryConfiguration || reason != "identity.transaction_rolled_back" {
			t.Fatalf("operation=%s category=%s reason=%s", operation, category, reason)
		}
	}
}
