package audit

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestBuildDiffSummaryRejectsEncodedSizeOverflow(t *testing.T) {
	fields := make([]string, maxAuditJSONFields)
	for i := range fields {
		fields[i] = strings.Repeat("a", 240) + fmt.Sprintf("%014d", i)
	}
	if _, err := BuildDiffSummary("updated", fields); err == nil {
		t.Fatal("oversized diff summary unexpectedly passed")
	}
}

func TestAuditSanitizerRedactsSensitiveKeysAndValuesAcrossWritePaths(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	const sentinel = "S0-04-04-SENTINEL"
	keys := []string{"password", "passphrase", "secret", "token", "credential", "api-key", "client_secret", "cookie", "authorization", "session", "csrf", "assertion", "signature"}
	values := []string{"Bearer " + sentinel, "Basic " + sentinel, "eyJ" + sentinel + ".aaa.bbb", "svc." + sentinel}
	for index, key := range keys {
		entry := completeEntry("redact-" + string(rune('a'+index)))
		entry.Metadata = `{"nested":[{"` + strings.ToUpper(key) + `":"` + values[index%len(values)] + `"}],"safe":900719925474099312345}`
		if _, err := store.Append(t.Context(), entry); err != nil {
			t.Fatalf("append %s: %v", key, err)
		}
	}
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := completeEntry("redact-transaction")
	entry.Metadata = `{"client.secret":"` + sentinel + `"}`
	if _, err := store.AppendInTransaction(t.Context(), tx, entry); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query(`SELECT id, event_category, organization_id, project_id, actor_type, actor_id, operation, authorization_decision, scope_type, scope_id, target_type, target_id, result, reason_code, trace_id, request_id, correlation_id, diff_summary, metadata, occurred_at, recorded_at FROM iotd_audit_entries`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		values := make([]sql.NullString, 21)
		destinations := make([]any, len(values))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if strings.Contains(value.String, sentinel) {
				t.Fatalf("sentinel leaked in text column: %q", value.String)
			}
		}
	}
	var metadata string
	if err := database.QueryRow(`SELECT metadata FROM iotd_audit_entries WHERE id = 'redact-a'`).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(metadata, "900719925474099312345") {
		t.Fatalf("large integer changed: %s", metadata)
	}
}

func TestAuditEntryRejectsCredentialInTopLevelTextFields(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	const sentinel = "svc.S0-04-04-top-level"
	for _, mutate := range []func(*Entry){func(e *Entry) { e.ID = sentinel }, func(e *Entry) { e.OrganizationID = sentinel }, func(e *Entry) { e.ProjectID = sentinel }, func(e *Entry) { e.ActorID = sentinel }, func(e *Entry) { e.Operation = sentinel }, func(e *Entry) { e.ScopeID = sentinel }, func(e *Entry) { e.TargetType = sentinel }, func(e *Entry) { e.TargetID = sentinel }, func(e *Entry) { e.ReasonCode = sentinel }, func(e *Entry) { e.TraceID = "Bearer " + sentinel }, func(e *Entry) { e.RequestID = sentinel }, func(e *Entry) { e.CorrelationID = sentinel }} {
		entry := completeEntry("top-" + strings.ReplaceAll(sentinel, ".", "-"))
		mutate(&entry)
		if _, err := store.Append(t.Context(), entry); err == nil || strings.Contains(err.Error(), sentinel) {
			t.Fatalf("top-level secret result=%v", err)
		}
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("top-level rejected rows=%d err=%v", count, err)
	}
}

func TestAuditSanitizerReplacesEntireCredentialValue(t *testing.T) {
	for _, value := range []string{"Bearer S0-04-04-SENTINEL", "Basic S0-04-04-SENTINEL", "eyJS0-04-04-SENTINEL.aaa.bbb", "svc.S0-04-04-SENTINEL"} {
		got, err := normalizeAuditJSON(`{"value":"` + value + `"}`)
		if err != nil || got != `{"value":"[REDACTED]"}` {
			t.Fatalf("value %q normalized as %q, %v", value, got, err)
		}
	}
}

func TestAuditEntryFailsClosedForUnboundObjectAndSystemScope(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Entry){func(e *Entry) { e.ScopeType = ScopeSystem; e.ScopeID = "" }, func(e *Entry) {
		e.ScopeType = ScopeObject
		e.OrganizationID = ""
		e.ProjectID = ""
		e.ScopeID = "object-a"
	}} {
		entry := completeEntry("scope-test")
		mutate(&entry)
		if _, err := store.Append(t.Context(), entry); err == nil {
			t.Fatal("unbound scope unexpectedly persisted")
		}
	}
}

func TestAuditObjectScopeIsOrganizationVisibleAndMalformedSystemIsInvisible(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	entry := completeEntry("object-visible")
	entry.ScopeType = ScopeObject
	entry.ScopeID = "object-a"
	if _, err := store.Append(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	page, err := store.Query(t.Context(), Query{OrganizationID: "org-a"})
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("object query=%#v err=%v", page, err)
	}
	if _, err := database.Exec(`INSERT INTO iotd_audit_entries (id,schema_version,event_category,organization_id,actor_type,actor_id,operation,authorization_decision,scope_type,result,reason_code,diff_summary,metadata,occurred_at,recorded_at) VALUES ('malformed',1,'system','org-a','system','system-a','system.event','not_evaluated','system','success','system.ok','{}','{}','2026-09-04T00:00:00.000000000Z','2026-09-04T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []Query{{OrganizationID: "org-a"}, {SystemScope: true}} {
		page, err := store.Query(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Entries {
			if item.ID == "malformed" {
				t.Fatal("malformed system visible")
			}
		}
	}
}

func TestAuditSanitizerBoundariesFailClosedWithoutEcho(t *testing.T) {
	const sentinel = "S0-04-04-boundary"
	for _, value := range []string{`[]`, `"` + sentinel + `"`, `null`, `{"a":1} trailing`, `{"a":"` + strings.Repeat("x", maxAuditJSONBytes) + `"}`} {
		if _, err := normalizeAuditJSON(value); err == nil || strings.Contains(err.Error(), sentinel) {
			t.Fatalf("unsafe JSON %q error=%v", value[:min(len(value), 16)], err)
		}
	}
	deep := `0`
	for range maxAuditJSONDepth + 1 {
		deep = `{"a":` + deep + `}`
	}
	if _, err := normalizeAuditJSON(deep); err == nil {
		t.Fatal("deep JSON passed")
	}
	tooMany := `{`
	for i := 0; i <= maxAuditJSONFields; i++ {
		if i > 0 {
			tooMany += ","
		}
		tooMany += fmt.Sprintf(`"k%d":0`, i)
	}
	tooMany += `}`
	if _, err := normalizeAuditJSON(tooMany); err == nil {
		t.Fatal("node overflow passed")
	}
}

func TestValidateDiffSummaryCanonicalRejectsVariants(t *testing.T) {
	for _, value := range []string{`{"change":"updated","fields":[],"unknown":1}`, `{"change":"updated","before":"x","fields":[]}`, `{"change":"updated","after":"x","fields":[]}`, `{"change":"updated","value":"x","fields":[]}`, `{"change":"updated","change":"updated","fields":[]}`, `{"fields":[],"change":"updated"}`, ` {"change":"updated","fields":[]}`, `{"change":"","fields":[]}`, `{"change":"UPDATED","fields":[]}`, strings.Repeat("x", maxAuditJSONBytes+1)} {
		if err := ValidateDiffSummary(value); err == nil {
			t.Fatalf("accepted %q", value[:min(len(value), 32)])
		}
	}
	fields := make([]string, maxAuditJSONFields)
	for i := range fields {
		fields[i] = "a"
	}
	if _, err := BuildDiffSummary("updated", fields); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildDiffSummary("updated", append(fields, "b")); err == nil {
		t.Fatal("129 fields accepted")
	}
	if _, err := BuildDiffSummary("updated", []string{strings.Repeat("a", 256)}); err == nil {
		t.Fatal("long path accepted")
	}
}

func TestAuditSanitizerRedactsEverySensitiveKeyVariant(t *testing.T) {
	keys := []string{"password", "passphrase", "secret", "token", "credential", "api-key", "client-secret", "cookie", "authorization", "session", "csrf", "assertion", "signature"}
	variants := []func(string) string{strings.ToUpper, func(s string) string { return strings.ReplaceAll(s, "-", "_") }, func(s string) string { return strings.ReplaceAll(s, "-", ".") }, func(s string) string { return strings.ReplaceAll(s, "-", " ") }}
	for _, key := range keys {
		for _, variant := range variants {
			got, err := normalizeAuditJSON(`{"` + variant(key) + `":"S0-04-04-SENTINEL"}`)
			if err != nil || !strings.Contains(got, redactedValue) {
				t.Fatalf("key %q=%q err=%v", variant(key), got, err)
			}
		}
	}
}

func TestAuditSanitizerExactLimitsAndSeparatedKeys(t *testing.T) {
	deep := `0`
	for range maxAuditJSONDepth {
		deep = `{"a":` + deep + `}`
	}
	if _, err := normalizeAuditJSON(deep); err != nil {
		t.Fatalf("depth limit: %v", err)
	}
	nodes := `{`
	for i := 0; i < maxAuditJSONFields-1; i++ {
		if i > 0 {
			nodes += ","
		}
		nodes += fmt.Sprintf(`"k%d":0`, i)
	}
	nodes += `}`
	if _, err := normalizeAuditJSON(nodes); err != nil {
		t.Fatalf("node limit: %v", err)
	}
	for _, key := range []string{"password", "passphrase", "secret", "token", "credential", "apikey", "clientsecret", "cookie", "authorization", "session", "csrf", "assertion", "signature"} {
		separated := strings.ToUpper(strings.Join(strings.Split(key, ""), "-"))
		got, err := normalizeAuditJSON(`{"` + separated + `":"S0-04-04-SENTINEL"}`)
		if err != nil || got != `{"`+separated+`":"[REDACTED]"}` {
			t.Fatalf("key %s=%q err=%v", key, got, err)
		}
	}
	if _, err := BuildDiffSummary("updated", []string{strings.Repeat("a", 255)}); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildDiffSummary("updated", []string{strings.Repeat("a", 256)}); err == nil {
		t.Fatal("256 path accepted")
	}
}

func TestAuditSanitizerRejectsHTMLEscapedOutputOverflow(t *testing.T) {
	safe := `{"value":"` + strings.Repeat("<", 2000) + `"}`
	if len(safe) > maxAuditJSONBytes {
		t.Fatal("bad fixture")
	}
	if _, err := normalizeAuditJSON(safe); err != nil {
		t.Fatalf("small escaped JSON: %v", err)
	}
	overflow := `{"value":"` + strings.Repeat("<", 3000) + `"}`
	if len(overflow) > maxAuditJSONBytes {
		t.Fatal("bad fixture")
	}
	if _, err := normalizeAuditJSON(overflow); err == nil {
		t.Fatal("HTML-escaped output overflow passed")
	}
}
