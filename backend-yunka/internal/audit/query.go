package audit

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 100
	maxCursorLength = 1024
)

// Query constrains audit reads to an explicit organization or the system scope.
// It is a persistence capability, not a transport or authorization endpoint.
type Query struct {
	OrganizationID string
	SystemScope    bool
	ProjectID      string
	Category       EventCategory
	ActorType      ActorType
	ActorID        string
	Operation      string
	Result         Result
	ReasonCode     string
	TargetType     string
	TargetID       string
	TraceID        string
	RequestID      string
	CorrelationID  string
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
	Limit          int
	Cursor         string
}

type Page struct {
	Entries    []Entry
	NextCursor string
}

type queryCursor struct {
	OccurredAt  string `json:"occurredAt"`
	Sequence    int64  `json:"sequence"`
	MaxSequence int64  `json:"maxSequence"`
	Fingerprint string `json:"fingerprint"`
}

func (store *SQLiteStore) Query(ctx context.Context, query Query) (Page, error) {
	if store == nil || store.database == nil {
		return Page{}, errors.New("audit SQLite store is not configured")
	}
	if err := validateQuery(query); err != nil {
		return Page{}, err
	}
	limit := query.Limit
	if limit == 0 {
		limit = DefaultPageSize
	}
	fingerprint, err := queryFingerprint(query)
	if err != nil {
		return Page{}, errors.New("audit query is invalid")
	}
	maxSequence := int64(0)
	if query.Cursor == "" {
		if err := store.database.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM iotd_audit_entries`).Scan(&maxSequence); err != nil {
			return Page{}, fmt.Errorf("read audit snapshot: %w", err)
		}
	}
	args := make([]any, 0, 20)
	where := []string{"1 = 1"}
	add := func(column, value string) {
		if value != "" {
			where = append(where, column+" = ?")
			args = append(args, value)
		}
	}
	if query.SystemScope {
		where = append(where, "scope_type = 'system' AND organization_id IS NULL AND project_id IS NULL")
	} else {
		add("organization_id", query.OrganizationID)
		where = append(where, "scope_type <> 'system'")
	}
	add("project_id", query.ProjectID)
	add("event_category", string(query.Category))
	add("actor_type", string(query.ActorType))
	add("actor_id", query.ActorID)
	add("operation", query.Operation)
	add("result", string(query.Result))
	add("reason_code", query.ReasonCode)
	add("target_type", query.TargetType)
	add("target_id", query.TargetID)
	add("trace_id", query.TraceID)
	add("request_id", query.RequestID)
	add("correlation_id", query.CorrelationID)
	if query.OccurredAfter != nil {
		where = append(where, "occurred_at >= ?")
		args = append(args, formatUTCTime(*query.OccurredAfter))
	}
	if query.OccurredBefore != nil {
		where = append(where, "occurred_at <= ?")
		args = append(args, formatUTCTime(*query.OccurredBefore))
	}
	if query.Cursor != "" {
		cursor, err := decodeCursor(query.Cursor)
		if err != nil {
			return Page{}, err
		}
		if cursor.Fingerprint != fingerprint {
			return Page{}, errors.New("audit cursor does not match query")
		}
		maxSequence = cursor.MaxSequence
		where = append(where, "sequence <= ?")
		args = append(args, maxSequence)
		where = append(where, "(occurred_at < ? OR (occurred_at = ? AND sequence < ?))")
		args = append(args, cursor.OccurredAt, cursor.OccurredAt, cursor.Sequence)
	} else {
		where = append(where, "sequence <= ?")
		args = append(args, maxSequence)
	}
	args = append(args, limit+1)
	rows, err := store.database.QueryContext(ctx, `SELECT sequence, id, schema_version, event_category, COALESCE(organization_id, ''), COALESCE(project_id, ''), actor_type, COALESCE(actor_id, ''), operation, authorization_decision, scope_type, COALESCE(scope_id, ''), COALESCE(target_type, ''), COALESCE(target_id, ''), result, reason_code, COALESCE(trace_id, ''), COALESCE(request_id, ''), COALESCE(correlation_id, ''), diff_summary, metadata, occurred_at, recorded_at FROM iotd_audit_entries WHERE `+strings.Join(where, " AND ")+` ORDER BY occurred_at DESC, sequence DESC LIMIT ?`, args...)
	if err != nil {
		return Page{}, fmt.Errorf("query audit entries: %w", err)
	}
	defer rows.Close()
	page := Page{Entries: make([]Entry, 0, limit)}
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return Page{}, err
		}
		page.Entries = append(page.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate audit entries: %w", err)
	}
	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		last := page.Entries[len(page.Entries)-1]
		encoded, err := encodeCursor(last, maxSequence, fingerprint)
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = encoded
	}
	return page, nil
}

func validateQuery(query Query) error {
	if query.SystemScope == (query.OrganizationID != "") {
		return errors.New("audit query requires exactly one organization or system scope")
	}
	if !query.SystemScope {
		if err := validateIdentifier("audit query organization", query.OrganizationID, false); err != nil {
			return err
		}
	}
	if err := validateIdentifier("audit query project", query.ProjectID, true); err != nil {
		return err
	}
	if query.Category != "" && !isEventCategory(query.Category) {
		return errors.New("audit query category is invalid")
	}
	if query.ActorType != "" && !isActorType(query.ActorType) {
		return errors.New("audit query actor type is invalid")
	}
	if query.ActorType == ActorAnonymous && query.ActorID != "" {
		return errors.New("anonymous audit query actor must not have an id")
	} else if query.ActorType == ActorAnonymous {
		// Anonymous audit events intentionally have no actor identifier.
	} else if (query.ActorType == "") != (query.ActorID == "") {
		return errors.New("audit query actor must be paired")
	}
	if (query.TargetType == "") != (query.TargetID == "") {
		return errors.New("audit query target must be paired")
	}
	if query.Result != "" && !isResult(query.Result) {
		return errors.New("audit query result is invalid")
	}
	for name, value := range map[string]string{"audit query actor": query.ActorID, "audit query target": query.TargetID, "audit query trace": query.TraceID, "audit query request": query.RequestID, "audit query correlation": query.CorrelationID} {
		if err := validateIdentifier(name, value, true); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{"audit query operation": query.Operation, "audit query reason": query.ReasonCode, "audit query target type": query.TargetType} {
		if err := validateDottedIdentifier(name, value, true); err != nil {
			return err
		}
	}
	if query.ProjectID != "" && query.SystemScope {
		return errors.New("audit project query requires an organization")
	}
	if query.Limit < 0 || query.Limit > MaxPageSize {
		return errors.New("audit query limit is invalid")
	}
	if query.OccurredAfter != nil {
		if err := validateUTCTime("query start", *query.OccurredAfter); err != nil {
			return errors.New("audit query start time is invalid")
		}
	}
	if query.OccurredBefore != nil {
		if err := validateUTCTime("query end", *query.OccurredBefore); err != nil {
			return errors.New("audit query end time is invalid")
		}
	}
	if query.OccurredAfter != nil && query.OccurredBefore != nil && query.OccurredAfter.After(*query.OccurredBefore) {
		return errors.New("audit query time range is invalid")
	}
	return nil
}

func encodeCursor(entry Entry, maxSequence int64, fingerprint string) (string, error) {
	value, err := json.Marshal(queryCursor{OccurredAt: formatUTCTime(entry.OccurredAt), Sequence: entry.Sequence, MaxSequence: maxSequence, Fingerprint: fingerprint})
	if err != nil {
		return "", errors.New("encode audit cursor")
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func decodeCursor(value string) (queryCursor, error) {
	if value == "" || len(value) > maxCursorLength {
		return queryCursor{}, errors.New("audit cursor is invalid")
	}
	encoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return queryCursor{}, errors.New("audit cursor is invalid")
	}
	if base64.RawURLEncoding.EncodeToString(encoded) != value {
		return queryCursor{}, errors.New("audit cursor is invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	var cursor queryCursor
	if decoder.Decode(&cursor) != nil || decoder.Decode(&struct{}{}) != io.EOF || cursor.Sequence <= 0 || cursor.MaxSequence < cursor.Sequence || cursor.Fingerprint == "" {
		return queryCursor{}, errors.New("audit cursor is invalid")
	}
	parsed, err := time.Parse("2006-01-02T15:04:05.000000000Z", cursor.OccurredAt)
	if err != nil || formatUTCTime(parsed) != cursor.OccurredAt {
		return queryCursor{}, errors.New("audit cursor is invalid")
	}
	fingerprint, err := base64.RawURLEncoding.DecodeString(cursor.Fingerprint)
	if err != nil || len(fingerprint) != sha256.Size || base64.RawURLEncoding.EncodeToString(fingerprint) != cursor.Fingerprint {
		return queryCursor{}, errors.New("audit cursor is invalid")
	}
	canonical, err := json.Marshal(cursor)
	if err != nil || string(canonical) != string(encoded) {
		return queryCursor{}, errors.New("audit cursor is invalid")
	}
	return cursor, nil
}

func queryFingerprint(query Query) (string, error) {
	copy := query
	copy.Cursor = ""
	copy.Limit = 0
	encoded, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

type rowScanner interface{ Scan(...any) error }

func scanEntry(row rowScanner) (Entry, error) {
	var entry Entry
	var eventCategory, actorType, decision, scopeType, result, occurredAt, recordedAt string
	if err := row.Scan(&entry.Sequence, &entry.ID, &entry.SchemaVersion, &eventCategory, &entry.OrganizationID, &entry.ProjectID, &actorType, &entry.ActorID, &entry.Operation, &decision, &scopeType, &entry.ScopeID, &entry.TargetType, &entry.TargetID, &result, &entry.ReasonCode, &entry.TraceID, &entry.RequestID, &entry.CorrelationID, &entry.DiffSummary, &entry.Metadata, &occurredAt, &recordedAt); err != nil {
		return Entry{}, fmt.Errorf("scan audit entry: %w", err)
	}
	entry.EventCategory = EventCategory(eventCategory)
	entry.ActorType = ActorType(actorType)
	entry.AuthorizationDecision = AuthorizationDecision(decision)
	entry.ScopeType = ScopeType(scopeType)
	entry.Result = Result(result)
	var err error
	entry.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
	if err != nil {
		return Entry{}, errors.New("parse audit occurred at")
	}
	entry.RecordedAt, err = time.Parse(time.RFC3339Nano, recordedAt)
	if err != nil {
		return Entry{}, errors.New("parse audit recorded at")
	}
	return entry, nil
}
