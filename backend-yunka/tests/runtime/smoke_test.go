//go:build yu31 && linux

package runtime_test

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localbffhttp"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localprojectroleadmin"
	"github.com/hvritual/yunka.io/framework/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	_ "modernc.org/sqlite"
)

type fixture struct {
	OrganizationID           string `json:"organizationId"`
	AdminUserID              string `json:"adminUserId"`
	AdminPassword            string `json:"adminPassword"`
	MemberUserID             string `json:"memberUserId"`
	MemberPassword           string `json:"memberPassword"`
	LocalAuthJWTKey          string `json:"localAuthJwtKey"`
	BFFAssertionKey          string `json:"bffAssertionKey"`
	MemberUserRevision       int64  `json:"memberUserRevision"`
	MemberCredentialRevision int64  `json:"memberCredentialRevision"`
}
type login struct {
	AccessToken    string `json:"accessToken"`
	UserID         string `json:"userId"`
	OrganizationID string `json:"organizationId"`
	CSRF           string `json:"csrfToken"`
	cookies        []*http.Cookie
	session        string
}

func authenticate(t *testing.T, ctx context.Context, origin string, f fixture, user, password string) *login {
	t.Helper()
	r := request(t, ctx, origin, "POST", "/auth/local/login", "", nil, map[string]string{"organizationId": f.OrganizationID, "userId": user, "password": password})
	requireStatus(t, r, 200)
	result := decode[login](t, r.body)
	result.cookies = r.cookies
	for _, c := range r.cookies {
		if c.Name == localbffhttp.SessionCookieName {
			result.session = c.Value
			if !c.HttpOnly || !c.Secure {
				t.Fatal("session cookie lost security attributes")
			}
		}
	}
	if result.UserID != user || result.OrganizationID != f.OrganizationID || result.AccessToken == "" || result.session == "" || result.CSRF == "" {
		t.Fatal("login did not establish requested durable identity")
	}
	return &result
}
func diagnostics(t *testing.T, ctx context.Context, origin string) {
	t.Helper()
	r := request(t, ctx, origin, "GET", "/__yunka/diagnostics", "", nil, nil)
	requireStatus(t, r, 200)
	report := decode[struct {
		Core core.DiagnosticsReport `json:"core"`
	}](t, r.body).Core
	if report.State != "ready" || !report.Health.Ready || report.Runtime.RPCServerCount != 1 {
		t.Fatalf("runtime diagnostics not ready: state=%s", report.State)
	}
	checks := map[string]core.HealthStatus{}
	for _, c := range report.Health.Checks {
		checks[c.Name] = c.Status
	}
	for _, name := range []string{"composition.capabilities", "module.delivery-event-runtime", "runtime.grpc-server", "runtime.http-server"} {
		if checks[name] != core.HealthStatusHealthy {
			t.Fatalf("runtime check %s is not healthy", name)
		}
	}
	components := map[string]bool{}
	for _, c := range report.Components {
		components[c.Name] = c.Startable && c.Shutdownable && c.HealthChecked
	}
	if len(components) != 2 || !components["grpc-server"] || !components["http-server"] {
		t.Fatal("hosted lifecycle inventory incomplete")
	}
}
func rpcContext(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// A shared durable SQLite file is used by the production HTTP/gRPC executable
// and separate development-only stdio MCP executables. "development-only"
// constrains the transport deployment, NOT the identity: no API key is set.
func TestYU31RealRuntimeTransportAndClosure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	root := t.TempDir()
	dbPath := filepath.Join(root, "runtime.db")
	vault := filepath.Join(root, "vault")
	manifest := filepath.Join(root, "fixture.json")
	fixtureChild := startChild(t, ctx, executable(t, "YU31_FIXTURE_BIN"), cleanEnvironment(nil), "-db", dbPath, "-vault", vault, "-manifest", manifest)
	fixtureChild.wait(t, true)
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	f := decode[fixture](t, raw)
	if f.AdminUserID == "" || f.MemberUserID == "" || f.AdminUserID == f.MemberUserID {
		t.Fatal("fixture did not create two separate real users")
	}
	info, err := os.Stat(manifest)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("fixture manifest must be private")
	}
	httpAddr, grpcAddr := freeAddress(t), freeAddress(t)
	origin := "http://" + httpAddr
	base := map[string]string{
		"IOT_DELIVERY_YUNKA_DB": dbPath, "IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT": vault,
		"IOT_DELIVERY_RUNTIME_ENVIRONMENT": "production", "IOT_DELIVERY_BOOTSTRAP_MODE": "disabled",
		"IOT_DELIVERY_YUNKA_HTTP_ADDR": httpAddr, "IOT_DELIVERY_YUNKA_GRPC_ADDR": grpcAddr,
		"IOT_DELIVERY_BFF_ORGANIZATION_ID": f.OrganizationID, "IOT_DELIVERY_BFF_ASSERTION_KEY": f.BFFAssertionKey,
		"IOT_DELIVERY_LOCAL_AUTH_JWT_KEY": f.LocalAuthJWTKey, "IOT_DELIVERY_DUE_REMINDER_INTERVAL": "1s",
	}
	main := startChild(t, ctx, executable(t, "YU31_RUNTIME_BIN"), cleanEnvironment(base))
	awaitReady(t, ctx, main, origin)
	diagnostics(t, ctx, origin)
	t.Log("YU31: production runtime ready; complete healthy capability/HTTP/gRPC lifecycle inventory")
	admin := authenticate(t, ctx, origin, f, f.AdminUserID, f.AdminPassword)
	member := authenticate(t, ctx, origin, f, f.MemberUserID, f.MemberPassword)
	if admin.session == member.session {
		t.Fatal("independent users share session")
	}
	transport, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	rpc := deliveryv1.NewDeliveryServiceClient(transport)
	mcpHTTP, mcpGRPC := freeAddress(t), freeAddress(t)
	mcpEnv := func(session string, ha, ga, environment string) []string {
		values := map[string]string{}
		for k, v := range base {
			values[k] = v
		}
		values["IOT_DELIVERY_RUNTIME_ENVIRONMENT"] = environment
		values["IOT_DELIVERY_MCP_SESSION_TOKEN"] = session
		values["IOT_DELIVERY_MCP_HTTP_ADDR"] = ha
		values["IOT_DELIVERY_MCP_GRPC_ADDR"] = ga
		return cleanEnvironment(values)
	}
	adminMCP := newMCP(t, ctx, mcpEnv(admin.session, mcpHTTP, mcpGRPC, "development"))
	memberHTTP, memberGRPC := freeAddress(t), freeAddress(t)
	memberMCP := newMCP(t, ctx, mcpEnv(member.session, memberHTTP, memberGRPC, "development"))
	diagnostics(t, ctx, "http://"+mcpHTTP)
	diagnostics(t, ctx, "http://"+memberHTTP)

	boundResponse := request(t, ctx, origin, "POST", "/api/projects", admin.AccessToken, nil, map[string]any{"name": "YU31 Bound", "board": "研发交付效能", "owner": f.AdminUserID})
	requireStatus(t, boundResponse, 201)
	bound := decode[delivery.Project](t, boundResponse.body)
	otherResponse := request(t, ctx, origin, "POST", "/api/projects", admin.AccessToken, nil, map[string]any{"name": "YU31 Unbound", "board": "产品与平台能力", "owner": f.AdminUserID})
	requireStatus(t, otherResponse, 201)
	other := decode[delivery.Project](t, otherResponse.body)
	if bound.ID == "" || other.ID == "" || bound.ID == other.ID {
		t.Fatal("distinct durable projects required")
	}
	checkAdmin := func() {
		t.Helper()
		r := request(t, ctx, origin, "GET", "/api/projects", admin.AccessToken, nil, nil)
		requireStatus(t, r, 200)
		if len(decode[[]delivery.Project](t, r.body)) != 2 {
			t.Fatal("HTTP lost durable projects")
		}
		out, err := rpc.ListProjects(rpcContext(ctx, admin.AccessToken), &deliveryv1.ListProjectsRequest{})
		if err != nil || len(out.GetProjects()) != 2 {
			t.Fatalf("gRPC admin project smoke failed: %v", status.Code(err))
		}
		data := adminMCP.tool(t, ctx, "delivery.list_projects", map[string]any{}, "")
		if len(decode[struct {
			Projects []delivery.Project `json:"projects"`
		}](t, data).Projects) != 2 {
			t.Fatal("MCP admin project smoke failed")
		}
	}
	checkAdmin()
	checkDenied := func(wantHTTP int, wantGRPC codes.Code, wantMCP string) {
		t.Helper()
		requireStatus(t, request(t, ctx, origin, "GET", "/api/projects", member.AccessToken, nil, nil), wantHTTP)
		_, err := rpc.ListProjects(rpcContext(ctx, member.AccessToken), &deliveryv1.ListProjectsRequest{})
		if status.Code(err) != wantGRPC {
			t.Fatalf("gRPC denial=%s want=%s", status.Code(err), wantGRPC)
		}
		memberMCP.tool(t, ctx, "delivery.list_projects", map[string]any{}, wantMCP)
	}
	checkDenied(403, codes.PermissionDenied, "permission_denied")
	// No fixture SQL grants: the only grant comes from the existing admin BFF.
	assignment := request(t, ctx, origin, "POST", "/auth/local/admin/project-role-bindings", "", admin, map[string]string{"projectId": bound.ID, "userId": f.MemberUserID, "roleId": "contributor"})
	requireStatus(t, assignment, 201)
	binding := decode[localprojectroleadmin.BindingResult](t, assignment.body)
	if binding.Revision != 1 || binding.BindingID == "" {
		t.Fatal("unexpected binding CAS state")
	}
	visible := request(t, ctx, origin, "GET", "/api/projects", member.AccessToken, nil, nil)
	requireStatus(t, visible, 200)
	projects := decode[[]delivery.Project](t, visible.body)
	if len(projects) != 1 || projects[0].ID != bound.ID {
		t.Fatal("HTTP project scope leaked")
	}
	grpcProjects, err := rpc.ListProjects(rpcContext(ctx, member.AccessToken), &deliveryv1.ListProjectsRequest{})
	if err != nil || len(grpcProjects.GetProjects()) != 1 || grpcProjects.Projects[0].Id != bound.ID {
		t.Fatal("gRPC durable scope differs from HTTP")
	}
	mcpProjects := decode[struct {
		Projects []delivery.Project `json:"projects"`
	}](t, memberMCP.tool(t, ctx, "delivery.list_projects", map[string]any{}, ""))
	if len(mcpProjects.Projects) != 1 || mcpProjects.Projects[0].ID != bound.ID {
		t.Fatal("same MCP process failed fresh durable grant resolution")
	}
	t.Log("YU31: HTTP/gRPC/stdio MCP agree: no grant denied; assigned project visible; unbound project excluded")

	createdResponse := request(t, ctx, origin, "POST", "/api/items", admin.AccessToken, nil, map[string]any{"title": "YU31 durable transport smoke", "projectId": bound.ID, "board": "研发交付效能", "owner": f.AdminUserID, "kind": "task", "type": "task", "priority": "P1"})
	requireStatus(t, createdResponse, 201)
	item := decode[delivery.WorkItem](t, createdResponse.body)
	grpcItem, err := rpc.GetItem(rpcContext(ctx, member.AccessToken), &deliveryv1.GetItemRequest{Id: item.ID})
	if err != nil || grpcItem.GetItem().GetId() != item.ID || grpcItem.GetItem().GetRevision() != item.Revision {
		t.Fatal("gRPC did not read the exact HTTP-created revision")
	}
	mcpItem := decode[struct {
		Item delivery.WorkItem `json:"item"`
	}](t, memberMCP.tool(t, ctx, "delivery.get_work_item", map[string]string{"id": item.ID}, ""))
	if mcpItem.Item.ID != item.ID || mcpItem.Item.Revision != item.Revision {
		t.Fatal("MCP did not read the exact HTTP-created revision")
	}

	observer, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if _, err = observer.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		t.Fatal(err)
	}
	until(t, ctx, "transactional Outbox publication and projection", func() bool {
		var total, pending int
		if err := observer.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN status != 'published' THEN 1 ELSE 0 END),0) FROM iotd_outbox`).Scan(&total, &pending); err != nil {
			return false
		}
		projection, err := os.ReadFile(filepath.Join(vault, "10-交付管理", "01-规划", item.ID+"-规划.md"))
		return total > 0 && pending == 0 && err == nil && strings.Contains(string(projection), item.Title)
	})
	var auditCount int
	if err := observer.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_audit_entries WHERE actor_id=? AND organization_id=? AND result='success'`, f.AdminUserID, f.OrganizationID).Scan(&auditCount); err != nil || auditCount == 0 {
		t.Fatal("no successful durable admin audit facts")
	}
	t.Log("YU31: HTTP write readable over gRPC/MCP; durable audit present; Outbox drained; Obsidian projection materialized")

	revoked := request(t, ctx, origin, "POST", "/auth/local/admin/project-role-bindings/"+binding.BindingID+"/revoke", "", admin, map[string]int64{"expectedRevision": binding.Revision})
	requireStatus(t, revoked, 200)
	requireStatus(t, request(t, ctx, origin, "GET", "/auth/local/current", "", member, nil), 200)
	checkDenied(403, codes.PermissionDenied, "permission_denied")
	checkAdmin()
	reset := request(t, ctx, origin, "POST", "/auth/local/admin/members/"+f.MemberUserID+"/reset-credential", "", admin, map[string]any{"expectedUserRevision": f.MemberUserRevision, "expectedCredentialRevision": f.MemberCredentialRevision, "password": f.MemberPassword + "-reset"})
	requireStatus(t, reset, 200)
	checkDenied(401, codes.Unauthenticated, "unauthenticated")
	checkAdmin()
	t.Log("YU31: revoke removes authorization without logout; credential reset invalidates all three transports; admin unaffected")

	// EOF is a normal MCP shutdown path; SIGTERM is tested on another real MCP.
	_ = memberMCP.child.input.Close()
	memberMCP.child.wait(t, true)
	assertReleased(t, memberHTTP, memberGRPC)
	adminMCP.child.terminate(t)
	assertReleased(t, mcpHTTP, mcpGRPC)
	main.terminate(t)
	assertReleased(t, httpAddr, grpcAddr)
	// State survives full process termination; no in-memory fake can pass this.
	restarted := startChild(t, ctx, executable(t, "YU31_RUNTIME_BIN"), cleanEnvironment(base))
	awaitReady(t, ctx, restarted, origin)
	diagnostics(t, ctx, origin)
	requireStatus(t, request(t, ctx, origin, "GET", "/api/projects", admin.AccessToken, nil, nil), 200)
	requireStatus(t, request(t, ctx, origin, "GET", "/api/projects", member.AccessToken, nil, nil), 401)
	persisted := request(t, ctx, origin, "GET", "/api/items/"+item.ID, admin.AccessToken, nil, nil)
	requireStatus(t, persisted, 200)
	if decode[delivery.WorkItem](t, persisted.body).Revision != item.Revision {
		t.Fatal("restart changed durable work item")
	}
	if err := restarted.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	restarted.wait(t, true)
	assertReleased(t, httpAddr, grpcAddr)
	if _, err := observer.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal("SQLite checkpoint failed after all runtimes exited")
	}
	t.Log("YU31: MCP EOF/SIGTERM and runtime SIGTERM/SIGINT reaped; all six ports released; durable restart verified")

	occupied, err := net.Listen("tcp", httpAddr)
	if err != nil {
		t.Fatal(err)
	}
	failed := startChild(t, ctx, executable(t, "YU31_RUNTIME_BIN"), cleanEnvironment(base))
	failed.wait(t, false)
	_ = occupied.Close()
	assertReleased(t, httpAddr, grpcAddr)
	forbiddenMCP := startChild(t, ctx, executable(t, "YU31_MCP_BIN"), mcpEnv(admin.session, mcpHTTP, mcpGRPC, "production"))
	forbiddenMCP.wait(t, false)
	assertReleased(t, mcpHTTP, mcpGRPC)
	t.Log("YU31: occupied-listener startup fails without orphan; production stdio MCP remains rejected")

	// Secrets are inspected for absence but never placed in failing assertion text.
	for _, process := range []*child{fixtureChild, main, adminMCP.child, memberMCP.child, restarted, failed, forbiddenMCP} {
		for _, secret := range []string{f.AdminPassword, f.MemberPassword, f.LocalAuthJWTKey, f.BFFAssertionKey, admin.AccessToken, member.AccessToken, admin.session, member.session} {
			if secret != "" && strings.Contains(process.output.String(), secret) {
				t.Fatal("runtime secret found in captured process log")
			}
		}
	}
	t.Log("YU31 RUNTIME SMOKE PASS")
}
func until(t *testing.T, ctx context.Context, label string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("context expired: " + label)
		case <-time.After(40 * time.Millisecond):
		}
	}
	t.Fatal(fmt.Sprintf("deadline exceeded: %s", label))
}
