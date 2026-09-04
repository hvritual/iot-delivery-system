// Package audit defines the append-only audit record contract and its SQLite
// persistence port. It deliberately does not subscribe to any business path.
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SchemaVersion is the only supported schema version for new audit entries.
const SchemaVersion = 1

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
		if entry.ScopeID != "" {
			return errors.New("system audit scope must not have a scope id")
		}
	case ScopeOrganization:
		if entry.OrganizationID == "" || entry.ScopeID != entry.OrganizationID {
			return errors.New("organization audit scope must match organization id")
		}
	case ScopeProject:
		if entry.OrganizationID == "" || entry.ProjectID == "" || entry.ScopeID != entry.ProjectID {
			return errors.New("project audit scope must match project and organization ids")
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
	if err := validateJSONObject("diff summary", entry.DiffSummary); err != nil {
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

func validateIdentifier(name, value string, optional bool) error {
	if value == "" && optional {
		return nil
	}
	if value == "" || value != strings.TrimSpace(value) || len(value) > 255 {
		return fmt.Errorf("%s must be a non-empty normalized identifier", name)
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
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &object); err != nil || object == nil {
		return fmt.Errorf("audit %s must be a valid JSON object", name)
	}
	return rejectSensitiveJSON(object)
}

func rejectSensitiveJSON(object map[string]json.RawMessage) error {
	for key, value := range object {
		lowerKey := strings.ToLower(key)
		if strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "credential") || strings.Contains(lowerKey, "password") || strings.Contains(lowerKey, "secret") || strings.Contains(lowerKey, "cookie") || strings.Contains(lowerKey, "authorization") {
			return fmt.Errorf("audit metadata key %q is sensitive", key)
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(value, &nested) == nil && nested != nil {
			if err := rejectSensitiveJSON(nested); err != nil {
				return err
			}
		}
	}
	return nil
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
