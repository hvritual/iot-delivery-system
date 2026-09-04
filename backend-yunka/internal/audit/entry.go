// Package audit defines the append-only audit record contract and its SQLite
// persistence port. It deliberately does not subscribe to any business path.
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
)

// SchemaVersion is the only supported schema version for new audit entries.
const SchemaVersion = 1

const redactedValue = "[REDACTED]"

const (
	maxAuditJSONBytes  = 16 << 10
	maxAuditJSONDepth  = 16
	maxAuditJSONFields = 128
)

var secretValuePattern = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9._~+/=-]+|basic\s+[a-z0-9+/=]+|\beyJ[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+\b|\bsvc\.[a-z0-9._-]+)`)

type EventCategory string

const (
	EventCategoryAuthentication EventCategory = "authentication"
	EventCategoryAuthorization  EventCategory = "authorization"
	EventCategoryDelivery       EventCategory = "delivery"
	EventCategoryConfiguration  EventCategory = "configuration"
	EventCategorySystem         EventCategory = "system"
	EventCategoryLegacy         EventCategory = "legacy"
)

type ActorType string

const (
	ActorHuman     ActorType = "human"
	ActorService   ActorType = "service"
	ActorSystem    ActorType = "system"
	ActorAnonymous ActorType = "anonymous"
	ActorLegacy    ActorType = "legacy"
)

type AuthorizationDecision string

const (
	DecisionAllowed      AuthorizationDecision = "allowed"
	DecisionDenied       AuthorizationDecision = "denied"
	DecisionNotEvaluated AuthorizationDecision = "not_evaluated"
)

type ScopeType string

const (
	ScopeOrganization ScopeType = "organization"
	ScopeProject      ScopeType = "project"
	ScopeObject       ScopeType = "object"
	ScopeSystem       ScopeType = "system"
)

type Result string

const (
	ResultSuccess Result = "success"
	ResultFailure Result = "failure"
	ResultDenied  Result = "denied"
)

// Entry is a stable, append-only event record. Optional identity and target
// fields intentionally support unauthenticated failure events without fake
// foreign-key identities.
type Entry struct {
	ID                    string
	Sequence              int64
	SchemaVersion         int
	EventCategory         EventCategory
	OrganizationID        string
	ProjectID             string
	ActorType             ActorType
	ActorID               string
	Operation             string
	AuthorizationDecision AuthorizationDecision
	ScopeType             ScopeType
	ScopeID               string
	TargetType            string
	TargetID              string
	Result                Result
	ReasonCode            string
	TraceID               string
	RequestID             string
	CorrelationID         string
	DiffSummary           string
	Metadata              string
	OccurredAt            time.Time
	RecordedAt            time.Time
}

func (entry Entry) validate() error {
	if entry.Sequence != 0 {
		return errors.New("audit sequence is assigned by SQLite")
	}
	if err := validateIdentifier("audit id", entry.ID, false); err != nil {
		return err
	}
	if entry.SchemaVersion != SchemaVersion {
		return fmt.Errorf("audit schema version = %d, want %d", entry.SchemaVersion, SchemaVersion)
	}
	if !isEventCategory(entry.EventCategory) || !isActorType(entry.ActorType) || !isDecision(entry.AuthorizationDecision) || !isScopeType(entry.ScopeType) || !isResult(entry.Result) {
		return errors.New("audit entry contains an invalid normalized enum")
	}
	if err := validateIdentifier("organization id", entry.OrganizationID, true); err != nil {
		return err
	}
	if err := validateIdentifier("project id", entry.ProjectID, true); err != nil {
		return err
	}
	if err := validateIdentifier("actor id", entry.ActorID, true); err != nil {
		return err
	}
	if entry.ActorType == ActorAnonymous && entry.ActorID != "" {
		return errors.New("anonymous audit actor must not have an actor id")
	}
	if entry.ActorType != ActorAnonymous && entry.ActorID == "" {
		return errors.New("identified audit actor requires an actor id")
	}
	if entry.ProjectID != "" && entry.OrganizationID == "" {
		return errors.New("audit project id requires an organization id")
	}
	if err := validateDottedIdentifier("operation", entry.Operation, false); err != nil {
		return err
	}
	if err := validateIdentifier("scope id", entry.ScopeID, entry.ScopeType == ScopeSystem); err != nil {
		return err
	}
	switch entry.ScopeType {
	case ScopeSystem:
		if entry.ScopeID != "" || entry.OrganizationID != "" || entry.ProjectID != "" {
			return errors.New("system audit scope must not have organization, project, or scope id")
		}
	case ScopeOrganization:
		if entry.OrganizationID == "" || entry.ScopeID != entry.OrganizationID {
			return errors.New("organization audit scope must match organization id")
		}
	case ScopeProject:
		if entry.OrganizationID == "" || entry.ProjectID == "" || entry.ScopeID != entry.ProjectID {
			return errors.New("project audit scope must match project and organization ids")
		}
	case ScopeObject:
		if entry.OrganizationID == "" || entry.ScopeID == "" || entry.TargetType == "" || entry.TargetID == "" {
			return errors.New("object audit scope requires organization, scope id, and target")
		}
	}
	if err := validateDottedIdentifier("target type", entry.TargetType, true); err != nil {
		return err
	}
	if err := validateIdentifier("target id", entry.TargetID, entry.TargetType == ""); err != nil {
		return err
	}
	if (entry.TargetType == "") != (entry.TargetID == "") {
		return errors.New("audit target type and id must be set together")
	}
	if entry.AuthorizationDecision == DecisionDenied && entry.Result != ResultDenied {
		return errors.New("denied authorization decision requires denied result")
	}
	if entry.Result == ResultDenied && entry.AuthorizationDecision != DecisionDenied {
		return errors.New("denied result requires denied authorization decision")
	}
	if err := validateDottedIdentifier("reason code", entry.ReasonCode, false); err != nil {
		return err
	}
	for name, value := range map[string]string{"trace id": entry.TraceID, "request id": entry.RequestID, "correlation id": entry.CorrelationID} {
		if err := validateIdentifier(name, value, true); err != nil {
			return err
		}
	}
	if err := ValidateDiffSummary(entry.DiffSummary); err != nil {
		return err
	}
	if err := validateJSONObject("metadata", entry.Metadata); err != nil {
		return err
	}
	if err := validateUTCTime("occurred at", entry.OccurredAt); err != nil {
		return err
	}
	if !entry.RecordedAt.IsZero() {
		return errors.New("audit recorded at is assigned by the store")
	}
	return nil
}

// BuildDiffSummary is the only builder for persisted audit diffs. It records
// stable change labels and field paths, never before/after values.
func BuildDiffSummary(change string, fields []string) (string, error) {
	if err := validateIdentifier("audit change", change, false); err != nil {
		return "", err
	}
	if change != strings.ToLower(change) {
		return "", errors.New("audit change must be lowercase")
	}
	if len(fields) > maxAuditJSONFields {
		return "", errors.New("audit diff has too many fields")
	}
	unique := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if err := validateAuditFieldPath(field); err != nil {
			return "", err
		}
		unique[field] = struct{}{}
	}
	normalized := slices.Sorted(maps.Keys(unique))
	if normalized == nil {
		normalized = []string{}
	}
	encoded, err := json.Marshal(struct {
		Change string   `json:"change"`
		Fields []string `json:"fields"`
	}{Change: change, Fields: normalized})
	if err != nil {
		return "", errors.New("encode audit diff summary")
	}
	if len(encoded) > maxAuditJSONBytes {
		return "", errors.New("audit diff summary is too large")
	}
	return string(encoded), nil
}

func ValidateDiffSummary(value string) error {
	if len(value) == 0 || len(value) > maxAuditJSONBytes {
		return errors.New("audit diff summary must use the canonical builder")
	}
	var summary struct {
		Change string   `json:"change"`
		Fields []string `json:"fields"`
	}
	if err := json.Unmarshal([]byte(value), &summary); err != nil {
		return errors.New("audit diff summary must be a valid JSON object")
	}
	canonical, err := BuildDiffSummary(summary.Change, summary.Fields)
	if err != nil || canonical != value {
		return errors.New("audit diff summary must use the canonical builder")
	}
	return nil
}

func validateAuditFieldPath(value string) error {
	if value == "" || len(value) > 255 || value != strings.TrimSpace(value) || strings.Contains(value, "..") || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return errors.New("audit diff field path must be normalized")
	}
	for _, part := range strings.Split(value, ".") {
		if err := validateIdentifier("audit diff field path", part, false); err != nil {
			return errors.New("audit diff field path must be normalized")
		}
	}
	return nil
}

func validateIdentifier(name, value string, optional bool) error {
	if value == "" && optional {
		return nil
	}
	if value == "" || value != strings.TrimSpace(value) || len(value) > 255 {
		return fmt.Errorf("%s must be a non-empty normalized identifier", name)
	}
	if secretValuePattern.MatchString(value) {
		return fmt.Errorf("%s must not contain a credential", name)
	}
	for index, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("._:/-", character)) {
			return fmt.Errorf("%s has invalid character at position %d", name, index)
		}
	}
	return nil
}

func validateDottedIdentifier(name, value string, optional bool) error {
	if err := validateIdentifier(name, value, optional); err != nil || value == "" {
		return err
	}
	if value != strings.ToLower(value) || !strings.Contains(value, ".") || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return fmt.Errorf("%s must be a lowercase dotted identifier", name)
	}
	return nil
}

func validateJSONObject(name, value string) error {
	if len(value) == 0 || len(value) > maxAuditJSONBytes {
		return fmt.Errorf("audit %s must be a valid JSON object", name)
	}
	object, err := decodeAuditJSONObject(value)
	if err != nil || object == nil {
		return fmt.Errorf("audit %s must be a valid JSON object", name)
	}
	return validateAuditJSON(object, 0, new(int))
}

func normalizeAuditJSON(value string) (string, error) {
	if len(value) == 0 || len(value) > maxAuditJSONBytes {
		return "", errors.New("audit JSON is unsafe")
	}
	object, err := decodeAuditJSONObject(value)
	if err != nil || object == nil {
		return "", errors.New("audit JSON is unsafe")
	}
	count := 0
	normalized, err := redactAuditJSON(object, 0, &count)
	if err != nil {
		return "", errors.New("audit JSON is unsafe")
	}
	encoded, err := json.Marshal(normalized)
	if err != nil || len(encoded) > maxAuditJSONBytes {
		return "", errors.New("audit JSON is unsafe")
	}
	return string(encoded), nil
}

func redactAuditJSON(value any, depth int, count *int) (any, error) {
	if depth > maxAuditJSONDepth {
		return nil, errors.New("audit JSON depth")
	}
	*count++
	if *count > maxAuditJSONFields {
		return nil, errors.New("audit JSON fields")
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			if isSensitiveAuditKey(key) {
				result[key] = redactedValue
				continue
			}
			value, err := redactAuditJSON(nested, depth+1, count)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, nested := range typed {
			value, err := redactAuditJSON(nested, depth+1, count)
			if err != nil {
				return nil, err
			}
			result[index] = value
		}
		return result, nil
	case string:
		if secretValuePattern.MatchString(typed) {
			return redactedValue, nil
		}
		return typed, nil
	case bool, json.Number, nil:
		return typed, nil
	default:
		return nil, errors.New("audit JSON type")
	}
}

func decodeAuditJSONObject(value string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, errors.New("audit JSON object")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("audit JSON trailing data")
	}
	return object, nil
}

func validateAuditJSON(value any, depth int, count *int) error {
	_, err := redactAuditJSON(value, depth, count)
	return err
}

func isSensitiveAuditKey(value string) bool {
	normalized := strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, strings.ToLower(value))
	for _, key := range []string{"password", "passphrase", "secret", "token", "credential", "apikey", "clientsecret", "cookie", "authorization", "session", "csrf", "assertion", "signature"} {
		if strings.Contains(normalized, key) {
			return true
		}
	}
	return false
}

func validateUTCTime(name string, value time.Time) error {
	if value.IsZero() || value.Location() != time.UTC {
		return fmt.Errorf("audit %s must be a non-zero UTC time", name)
	}
	return nil
}

func isEventCategory(value EventCategory) bool {
	return value == EventCategoryAuthentication || value == EventCategoryAuthorization || value == EventCategoryDelivery || value == EventCategoryConfiguration || value == EventCategorySystem || value == EventCategoryLegacy
}

func isActorType(value ActorType) bool {
	return value == ActorHuman || value == ActorService || value == ActorSystem || value == ActorAnonymous || value == ActorLegacy
}

func isDecision(value AuthorizationDecision) bool {
	return value == DecisionAllowed || value == DecisionDenied || value == DecisionNotEvaluated
}

func isScopeType(value ScopeType) bool {
	return value == ScopeOrganization || value == ScopeProject || value == ScopeObject || value == ScopeSystem
}

func isResult(value Result) bool {
	return value == ResultSuccess || value == ResultFailure || value == ResultDenied
}
