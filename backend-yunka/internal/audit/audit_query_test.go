package audit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAuditQueryFiltersScopesPaginationAndCancellation(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	for index, stamp := range []time.Time{base.Add(900 * time.Millisecond), base.Add(100 * time.Millisecond), base} {
		entry := completeEntry("page-" + string(rune('a'+index)))
		entry.OccurredAt = stamp
		entry.Metadata = `{"safe":"kept","token":"secret"}`
		if _, err := store.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	entry := completeEntry("org-b")
	entry.OrganizationID, entry.ProjectID, entry.ScopeID = "org-b", "project-b", "project-b"
	if _, err := store.Append(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	system := completeEntry("system")
	system.OrganizationID, system.ProjectID, system.ScopeID, system.ScopeType = "", "", "", ScopeSystem
	system.ActorType, system.ActorID = ActorSystem, "system-a"
	if _, err := store.Append(t.Context(), system); err != nil {
		t.Fatal(err)
	}
	query := Query{OrganizationID: "org-a", ProjectID: "project-a", Category: EventCategoryAuthorization, ActorType: ActorHuman, ActorID: "user-a", Operation: "delivery.items.create", Result: ResultSuccess, ReasonCode: "authorization.allowed", TargetType: "delivery.item", TargetID: "item-a", TraceID: "trace-a", RequestID: "request-a", CorrelationID: "correlation-a", Limit: 1}
	seen := map[string]bool{}
	for {
		page, err := store.Query(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Entries {
			seen[item.ID] = true
			if item.Metadata != `{"safe":"kept","token":"[REDACTED]"}` && item.ID != "page-a" {
				t.Fatalf("unexpected metadata: %s", item.Metadata)
			}
		}
		if page.NextCursor == "" {
			break
		}
		if len(seen) == 1 {
			late := completeEntry("late")
			late.OccurredAt = base.Add(time.Hour)
			if _, err := store.Append(t.Context(), late); err != nil {
				t.Fatal(err)
			}
		}
		query.Cursor = page.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("pages = %#v", seen)
	}
	if page, err := store.Query(t.Context(), Query{SystemScope: true, Limit: 10}); err != nil || len(page.Entries) != 1 || page.Entries[0].ID != "system" {
		t.Fatalf("system page=%#v err=%v", page, err)
	}
	for _, bad := range []Query{{OrganizationID: "org-a", ActorType: ActorHuman}, {OrganizationID: "org-a", TargetID: "item-a"}, {OrganizationID: "org-a", Operation: "x' OR 1=1 --"}} {
		if _, err := store.Query(t.Context(), bad); err == nil {
			t.Fatalf("accepted %#v", bad)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Query(ctx, Query{OrganizationID: "org-a"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestDecodeCursorRejectsEveryMalformedVariant(t *testing.T) {
	entry := completeEntry("cursor")
	entry.Sequence = 7
	valid, err := encodeCursor(entry, 9, strings.Repeat("a", 43))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(valid)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	encode := func(values map[string]any) string {
		encoded, _ := json.Marshal(values)
		return base64.RawURLEncoding.EncodeToString(encoded)
	}
	variants := []string{strings.Repeat("a", maxCursorLength+1), valid + "=", base64.RawURLEncoding.EncodeToString(append(raw, []byte(` `)...)), base64.RawURLEncoding.EncodeToString(append(raw[:len(raw)-1], []byte(`,"x":1}`)...))}
	for _, name := range []string{"occurredAt", "sequence", "maxSequence", "fingerprint"} {
		copy := maps.Clone(object)
		delete(copy, name)
		variants = append(variants, encode(copy))
	}
	for _, mutate := range []func(map[string]any){func(v map[string]any) { v["sequence"] = 0 }, func(v map[string]any) { v["sequence"] = -1 }, func(v map[string]any) { v["maxSequence"] = 0 }, func(v map[string]any) { v["maxSequence"] = 1 }, func(v map[string]any) { v["occurredAt"] = "" }, func(v map[string]any) { v["occurredAt"] = "2026-09-04T00:00:00Z" }, func(v map[string]any) { v["occurredAt"] = "2026-09-04T08:00:00.000000000+08:00" }, func(v map[string]any) { v["fingerprint"] = "" }, func(v map[string]any) { v["fingerprint"] = "%%%" }, func(v map[string]any) { v["fingerprint"] = base64.RawURLEncoding.EncodeToString(make([]byte, 31)) }, func(v map[string]any) { v["fingerprint"] = base64.RawURLEncoding.EncodeToString(make([]byte, 33)) }, func(v map[string]any) { v["fingerprint"] = strings.Repeat("a", 43) + "=" }} {
		copy := maps.Clone(object)
		mutate(copy)
		variants = append(variants, encode(copy))
	}
	variants = append(variants, base64.RawURLEncoding.EncodeToString([]byte(`{"occurredAt":"2026-09-04T00:00:00.000000000Z","sequence":7,"sequence":7,"maxSequence":9,"fingerprint":"`+strings.Repeat("a", 43)+`"}`)), "S0-04-04-CURSOR-SENTINEL")
	for _, value := range variants {
		if _, err := decodeCursor(value); err == nil || strings.Contains(err.Error(), "S0-04-04-CURSOR-SENTINEL") {
			t.Fatalf("accepted/echoed cursor %q: %v", value[:min(24, len(value))], err)
		}
	}
}

func TestDecodeCursorRejectsCanonicalSemanticVariants(t *testing.T) {
	valid := queryCursor{OccurredAt: "2026-09-04T00:00:00.000000000Z", Sequence: 7, MaxSequence: 9, Fingerprint: strings.Repeat("a", 43)}
	encode := func(value queryCursor) string {
		raw, _ := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	for _, mutate := range []func(*queryCursor){func(v *queryCursor) { v.Sequence = 0 }, func(v *queryCursor) { v.Sequence = -1 }, func(v *queryCursor) { v.MaxSequence = 0 }, func(v *queryCursor) { v.MaxSequence = 1 }, func(v *queryCursor) { v.OccurredAt = "2026-09-04T00:00:00Z" }, func(v *queryCursor) { v.OccurredAt = "2026-09-04T08:00:00.000000000+08:00" }, func(v *queryCursor) { v.Fingerprint = "" }, func(v *queryCursor) { v.Fingerprint = base64.RawURLEncoding.EncodeToString(make([]byte, 31)) }, func(v *queryCursor) { v.Fingerprint = base64.RawURLEncoding.EncodeToString(make([]byte, 33)) }} {
		value := valid
		mutate(&value)
		if _, err := decodeCursor(encode(value)); err == nil {
			t.Fatal("canonical semantic variant accepted")
		}
	}
}

func TestAuditQueryEachFilterExcludesSingleFieldDistractor(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	base := completeEntry("filter-base")
	base.OccurredAt = time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	if _, err := store.Append(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		query  func() Query
		mutate func(*Entry)
	}{
		{"project", func() Query { return Query{OrganizationID: "org-a", ProjectID: "project-a"} }, func(e *Entry) { e.ProjectID = "project-b"; e.ScopeID = "project-b" }},
		{"category", func() Query { return Query{OrganizationID: "org-a", Category: EventCategoryAuthorization} }, func(e *Entry) { e.EventCategory = EventCategoryDelivery }},
		{"actor", func() Query { return Query{OrganizationID: "org-a", ActorType: ActorHuman, ActorID: "user-a"} }, func(e *Entry) { e.ActorID = "user-b" }},
		{"operation", func() Query { return Query{OrganizationID: "org-a", Operation: "delivery.items.create"} }, func(e *Entry) { e.Operation = "delivery.items.update" }},
		{"result", func() Query { return Query{OrganizationID: "org-a", Result: ResultSuccess} }, func(e *Entry) { e.Result = ResultFailure }},
		{"reason", func() Query { return Query{OrganizationID: "org-a", ReasonCode: "authorization.allowed"} }, func(e *Entry) { e.ReasonCode = "authorization.changed" }},
		{"target", func() Query { return Query{OrganizationID: "org-a", TargetType: "delivery.item", TargetID: "item-a"} }, func(e *Entry) { e.TargetID = "item-b" }},
		{"trace", func() Query { return Query{OrganizationID: "org-a", TraceID: "trace-a"} }, func(e *Entry) { e.TraceID = "trace-b" }},
		{"request", func() Query { return Query{OrganizationID: "org-a", RequestID: "request-a"} }, func(e *Entry) { e.RequestID = "request-b" }},
		{"correlation", func() Query { return Query{OrganizationID: "org-a", CorrelationID: "correlation-a"} }, func(e *Entry) { e.CorrelationID = "correlation-b" }},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := base
			entry.ID = "filter-" + string(rune('a'+index))
			test.mutate(&entry)
			if _, err := store.Append(t.Context(), entry); err != nil {
				t.Fatal(err)
			}
			page, err := store.Query(t.Context(), test.query())
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range page.Entries {
				if got.ID == entry.ID {
					t.Fatalf("%s filter ignored", test.name)
				}
			}
		})
	}
	after := base.OccurredAt.Add(time.Nanosecond)
	before := base.OccurredAt.Add(-time.Nanosecond)
	for _, query := range []Query{{OrganizationID: "org-a", OccurredAfter: &after}, {OrganizationID: "org-a", OccurredBefore: &before}} {
		page, err := store.Query(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		for _, got := range page.Entries {
			if got.ID == base.ID {
				t.Fatal("time filter ignored")
			}
		}
	}
	page, err := store.Query(t.Context(), Query{OrganizationID: "org-a", ProjectID: "project-a", Limit: 1})
	if err != nil || len(page.Entries) != 1 {
		t.Fatal(err)
	}
	byID, err := store.ByID(t.Context(), page.Entries[0].ID)
	if err != nil || !reflect.DeepEqual(byID, page.Entries[0]) {
		t.Fatalf("ByID mismatch: %#v %v", byID, err)
	}
}

func TestAuditCursorBindsEveryQueryDimension(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"cursor-a", "cursor-b"} {
		if _, err := store.Append(t.Context(), completeEntry(id)); err != nil {
			t.Fatal(err)
		}
	}
	base := Query{OrganizationID: "org-a", ProjectID: "project-a", Limit: 1}
	page, err := store.Query(t.Context(), base)
	if err != nil || page.NextCursor == "" {
		t.Fatal(err)
	}
	mutations := []func(*Query){func(q *Query) { q.OrganizationID = "org-b" }, func(q *Query) { q.SystemScope = true; q.OrganizationID = "" }, func(q *Query) { q.ProjectID = "project-b" }, func(q *Query) { q.Category = EventCategoryAuthorization }, func(q *Query) { q.ActorType = ActorHuman; q.ActorID = "user-a" }, func(q *Query) { q.Operation = "delivery.items.create" }, func(q *Query) { q.Result = ResultSuccess }, func(q *Query) { q.ReasonCode = "authorization.allowed" }, func(q *Query) { q.TargetType = "delivery.item"; q.TargetID = "item-a" }, func(q *Query) { q.TraceID = "trace-a" }, func(q *Query) { q.RequestID = "request-a" }, func(q *Query) { q.CorrelationID = "correlation-a" }, func(q *Query) { value := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC); q.OccurredAfter = &value }, func(q *Query) { value := time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC); q.OccurredBefore = &value }}
	for _, mutate := range mutations {
		query := base
		query.Cursor = page.NextCursor
		mutate(&query)
		if _, err := store.Query(t.Context(), query); err == nil {
			t.Fatal("cursor accepted changed query")
		}
	}
}

func TestAuditPaginationHasStableThreePageSnapshot(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	for _, value := range []struct {
		id string
		at time.Time
	}{{"page-a", base.Add(900 * time.Millisecond)}, {"page-b", base.Add(100 * time.Millisecond)}, {"page-c", base}} {
		entry := completeEntry(value.id)
		entry.OccurredAt = value.at
		if _, err := store.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	query := Query{OrganizationID: "org-a", ProjectID: "project-a", Limit: 1}
	got := []string{}
	for pageNumber := 0; ; pageNumber++ {
		page, err := store.Query(t.Context(), query)
		if err != nil || len(page.Entries) != 1 {
			t.Fatalf("page %d=%#v err=%v", pageNumber, page, err)
		}
		got = append(got, page.Entries[0].ID)
		if pageNumber == 0 {
			for _, id := range []string{"future", "backdated"} {
				entry := completeEntry(id)
				entry.OccurredAt = base.Add(time.Hour)
				if id == "backdated" {
					entry.OccurredAt = base.Add(-time.Hour)
				}
				if _, err := store.Append(t.Context(), entry); err != nil {
					t.Fatal(err)
				}
			}
		}
		if page.NextCursor == "" {
			break
		}
		query.Cursor = page.NextCursor
	}
	if !reflect.DeepEqual(got, []string{"page-a", "page-b", "page-c"}) {
		t.Fatalf("order=%v", got)
	}
}

func TestAuditQueryAllowsAnonymousActorFilterWithoutActorID(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{ID: "anonymous-query", SchemaVersion: SchemaVersion, EventCategory: EventCategoryAuthentication, ActorType: ActorAnonymous, Operation: "authentication.login", AuthorizationDecision: DecisionNotEvaluated, ScopeType: ScopeSystem, Result: ResultFailure, ReasonCode: "authentication.invalid", DiffSummary: `{"change":"rejected","fields":[]}`, Metadata: `{}`, OccurredAt: time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)}
	if _, err := store.Append(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	page, err := store.Query(t.Context(), Query{SystemScope: true, ActorType: ActorAnonymous})
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("anonymous query=%#v err=%v", page, err)
	}
}

func TestAuditQueryRejectsAnonymousActorID(t *testing.T) {
	store, err := NewSQLiteStore(openMigratedTestDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Query(t.Context(), Query{SystemScope: true, ActorType: ActorAnonymous, ActorID: "user-a"}); err == nil {
		t.Fatal("anonymous actor ID accepted")
	}
}

func TestAuditQueryDefaultAndMaximumLimits(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	for i := range MaxPageSize + 1 {
		entry := completeEntry("limit-" + string(rune('a'+i%26)) + string(rune('a'+i/26)))
		entry.OccurredAt = time.Date(2026, 9, 4, 0, 0, i, 0, time.UTC)
		if _, err := store.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.Query(t.Context(), Query{OrganizationID: "org-a"})
	if err != nil || len(page.Entries) != DefaultPageSize {
		t.Fatalf("default=%d err=%v", len(page.Entries), err)
	}
	page, err = store.Query(t.Context(), Query{OrganizationID: "org-a", Limit: MaxPageSize})
	if err != nil || len(page.Entries) != MaxPageSize || page.NextCursor == "" {
		t.Fatalf("maximum=%d cursor=%q err=%v", len(page.Entries), page.NextCursor, err)
	}
}
