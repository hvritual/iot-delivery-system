package localbffhttp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localprojectroleadmin"
)

func TestYU26AdminRoutesUseDurableManagerGuardCASAuditAndOutbox(t *testing.T) {
	fixture := newBFFFixture(t)
	adminCookie, csrf := fixture.loginCookies(t, "admin-a", "YU26-admin-password")
	headers := map[string]string{"Cookie": adminCookie, CSRFHeader: csrf}

	create := fixture.request(t, http.MethodPost, "/auth/local/admin/members", `{"displayName":"Managed User","email":"managed@example.test","password":"YU26-managed-password"}`, headers, true)
	if create.Code != http.StatusCreated {
		t.Fatalf("create member status=%d body=%s", create.Code, create.Body.String())
	}
	var created localmemberadmin.MemberResult
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.UserID == "" || created.OrganizationID != "org-a" || created.UserRevision != 1 || created.CredentialRevision != 1 {
		t.Fatalf("created member=%#v", created)
	}
	var userStatus string
	if err := fixture.database.QueryRow(`SELECT status FROM users WHERE organization_id = 'org-a' AND id = ?`, created.UserID).Scan(&userStatus); err != nil || userStatus != "active" {
		t.Fatalf("created user status=%q error=%v", userStatus, err)
	}

	assign := fixture.request(t, http.MethodPost, "/auth/local/admin/project-role-bindings", `{"projectId":"project-a","userId":"`+created.UserID+`","roleId":"contributor"}`, headers, true)
	if assign.Code != http.StatusCreated {
		t.Fatalf("assign role status=%d body=%s", assign.Code, assign.Body.String())
	}
	var binding localprojectroleadmin.BindingResult
	if err := json.Unmarshal(assign.Body.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}
	if binding.BindingID == "" || binding.ProjectID != "project-a" || binding.UserID != created.UserID || binding.RoleID != "contributor" || binding.Revision != 1 {
		t.Fatalf("binding=%#v", binding)
	}
	revoke := fixture.request(t, http.MethodPost, "/auth/local/admin/project-role-bindings/"+binding.BindingID+"/revoke", `{"expectedRevision":1}`, headers, true)
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke role status=%d body=%s", revoke.Code, revoke.Body.String())
	}
	var bindingStatus string
	var bindingRevision int64
	if err := fixture.database.QueryRow(`SELECT status, revision FROM role_bindings WHERE id = ?`, binding.BindingID).Scan(&bindingStatus, &bindingRevision); err != nil || bindingStatus != "disabled" || bindingRevision != 2 {
		t.Fatalf("revoked binding status=%s revision=%d error=%v", bindingStatus, bindingRevision, err)
	}

	reset := fixture.request(t, http.MethodPost, "/auth/local/admin/members/"+created.UserID+"/reset-credential", `{"expectedUserRevision":1,"expectedCredentialRevision":1,"password":"YU26-reset-password"}`, headers, true)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset credential status=%d body=%s", reset.Code, reset.Body.String())
	}
	verification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", created.UserID, []byte("YU26-reset-password"))
	if err != nil || !verification.Match || verification.Revision != 2 {
		t.Fatalf("reset credential verification=%#v error=%v", verification, err)
	}

	disable := fixture.request(t, http.MethodPost, "/auth/local/admin/members/"+created.UserID+"/disable", `{"expectedRevision":2}`, headers, true)
	if disable.Code != http.StatusOK {
		t.Fatalf("disable member status=%d body=%s", disable.Code, disable.Body.String())
	}
	if err := fixture.database.QueryRow(`SELECT status FROM users WHERE organization_id = 'org-a' AND id = ?`, created.UserID).Scan(&userStatus); err != nil || userStatus != "disabled" {
		t.Fatalf("disabled user status=%q error=%v", userStatus, err)
	}

	var outboxCount, auditCount int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_outbox`).Scan(&outboxCount); err != nil || outboxCount < 4 {
		t.Fatalf("outbox count=%d error=%v", outboxCount, err)
	}
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE actor_id = 'admin-a' AND result = 'success'`).Scan(&auditCount); err != nil || auditCount < 4 {
		t.Fatalf("admin success audit count=%d error=%v", auditCount, err)
	}

	userCookie, userCSRF := fixture.loginCookies(t, "user-a", "YU26-user-password")
	forged := fixture.request(t, http.MethodPost, "/auth/local/admin/members", `{"displayName":"Must Fail","password":"YU26-must-fail"}`, map[string]string{"Cookie": userCookie, CSRFHeader: userCSRF}, true)
	if forged.Code != http.StatusForbidden {
		t.Fatalf("non-admin create status=%d body=%s", forged.Code, forged.Body.String())
	}
}
