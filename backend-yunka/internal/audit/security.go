package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"yunka.io/framework/core/identity"
	"yunka.io/framework/core/runtimecontext"
)

// SecurityRecorder writes the security events whose outcome is decided before
// an application OperationPlan can start. It deliberately records only stable
// classifications and trusted runtime correlation values.
type SecurityRecorder struct {
	store Store
	newID func() (string, error)
	clock func() time.Time
}

// RecordAuthorizationDenied captures a completed, trusted authorization
// decision. Target fields are intentionally left empty because this shared
// boundary has not established server-owned resource membership yet.
func (recorder *SecurityRecorder) RecordAuthorizationDenied(ctx context.Context, operation string) error {
	actorType, actorID, organizationID, err := trustedSecurityActor(ctx)
	if err != nil {
		return err
	}
	return recorder.record(ctx, securityRecord{
		eventCategory:  EventCategoryAuthorization,
		actorType:      actorType,
		actorID:        actorID,
		organizationID: organizationID,
		operation:      operation,
		decision:       DecisionDenied,
		result:         ResultDenied,
		reasonCode:     "authorization.denied",
		transport:      securityTransport(ctx),
		phase:          "authorization",
		failureClass:   "authorization",
		change:         "denied",
	})
}

func NewSecurityRecorder(store Store) (*SecurityRecorder, error) {
	if store == nil {
		return nil, errors.New("security audit store is required")
	}
	return &SecurityRecorder{store: store, newID: randomSecurityAuditID, clock: time.Now}, nil
}

// RecordAuthenticationFailure preserves an anonymous failed credential check.
// Callers retain their existing public error category even if persistence is
// unavailable, so audit availability cannot turn a rejected credential into a
// successful request.
func (recorder *SecurityRecorder) RecordAuthenticationFailure(ctx context.Context, operation, transport, reasonCode string) error {
	return recorder.record(ctx, securityRecord{
		eventCategory: EventCategoryAuthentication,
		actorType:     ActorAnonymous,
		operation:     operation,
		decision:      DecisionNotEvaluated,
		result:        ResultFailure,
		reasonCode:    reasonCode,
		transport:     transport,
		phase:         "authentication",
		failureClass:  "credential",
		change:        "rejected",
	})
}

type securityRecord struct {
	eventCategory  EventCategory
	actorType      ActorType
	actorID        string
	organizationID string
	operation      string
	decision       AuthorizationDecision
	result         Result
	reasonCode     string
	transport      string
	phase          string
	failureClass   string
	change         string
}

// RecordAuthenticationAccepted records the backend's acceptance of an already
// verified BFF assertion. It does not claim to observe an upstream IdP callback.
func (recorder *SecurityRecorder) RecordAuthenticationAccepted(ctx context.Context, operation string) error {
	actorType, actorID, organizationID, err := trustedSecurityActor(ctx)
	if err != nil {
		return err
	}
	return recorder.record(ctx, securityRecord{
		eventCategory:  EventCategoryAuthentication,
		actorType:      actorType,
		actorID:        actorID,
		organizationID: organizationID,
		operation:      operation,
		decision:       DecisionNotEvaluated,
		result:         ResultSuccess,
		reasonCode:     "authentication.assertion_accepted",
		transport:      securityTransport(ctx),
		phase:          "authentication",
		failureClass:   "accepted",
		change:         "accepted",
	})
}

// RecordApplicationRollback records a failure only after the shared executor
// has returned from its local transaction, so the failure audit is not part of
// the transaction that was intentionally rolled back.
func (recorder *SecurityRecorder) RecordApplicationRollback(ctx context.Context, operation string) error {
	actorType, actorID, organizationID, err := trustedSecurityActor(ctx)
	if err != nil {
		return err
	}
	return recorder.record(ctx, securityRecord{
		eventCategory:  EventCategoryDelivery,
		actorType:      actorType,
		actorID:        actorID,
		organizationID: organizationID,
		operation:      operation,
		decision:       DecisionAllowed,
		result:         ResultFailure,
		reasonCode:     "application.transaction_rolled_back",
		transport:      securityTransport(ctx),
		phase:          "application",
		failureClass:   "transaction",
		change:         "rolled_back",
	})
}

// RecordRevocationInTransaction ensures the state change and audit entry are
// committed together. Only a verified actor context can initiate this write.
func (recorder *SecurityRecorder) RecordRevocationInTransaction(ctx context.Context, transaction *sql.Tx, operation, targetType, targetID, targetOrganizationID string) error {
	if recorder == nil || recorder.newID == nil || recorder.clock == nil {
		return errors.New("security audit recorder is not configured")
	}
	store, ok := recorder.store.(*SQLiteStore)
	if !ok {
		return errors.New("security revocation audit requires SQLite store")
	}
	actorType, actorID, organizationID, err := trustedSecurityActor(ctx)
	if err != nil {
		return err
	}
	if err := validateIdentifier("security audit revocation organization", targetOrganizationID, false); err != nil {
		return err
	}
	if organizationID != targetOrganizationID {
		return errors.New("security audit revocation target organization mismatch")
	}
	id, err := recorder.newID()
	if err != nil {
		return fmt.Errorf("generate security audit ID: %w", err)
	}
	metadata, err := json.Marshal(struct {
		Transport    string `json:"transport"`
		Phase        string `json:"phase"`
		FailureClass string `json:"failure_class"`
	}{Transport: securityTransport(ctx), Phase: "configuration", FailureClass: "revocation"})
	if err != nil {
		return fmt.Errorf("encode revocation audit metadata: %w", err)
	}
	requestID, correlationID := securityRuntimeCorrelation(ctx)
	occurredAt := recorder.clock().UTC()
	if occurredAt.IsZero() {
		return errors.New("security audit clock returned zero time")
	}
	_, err = store.AppendInTransaction(ctx, transaction, Entry{
		ID: id, SchemaVersion: SchemaVersion, EventCategory: EventCategoryConfiguration,
		OrganizationID: targetOrganizationID, ActorType: actorType, ActorID: actorID, Operation: operation,
		AuthorizationDecision: DecisionNotEvaluated, ScopeType: ScopeOrganization, ScopeID: targetOrganizationID,
		TargetType: targetType, TargetID: targetID, Result: ResultSuccess, ReasonCode: "configuration.revoked",
		TraceID: runtimecontext.TraceIDFrom(ctx), RequestID: requestID, CorrelationID: correlationID,
		DiffSummary: `{"change":"revoked"}`, Metadata: string(metadata), OccurredAt: occurredAt,
	})
	if err != nil {
		return fmt.Errorf("append revocation audit: %w", err)
	}
	return nil
}

func (recorder *SecurityRecorder) record(ctx context.Context, value securityRecord) error {
	if recorder == nil || recorder.store == nil || recorder.newID == nil || recorder.clock == nil {
		return errors.New("security audit recorder is not configured")
	}
	id, err := recorder.newID()
	if err != nil {
		return fmt.Errorf("generate security audit ID: %w", err)
	}
	metadata, err := json.Marshal(struct {
		Transport    string `json:"transport"`
		Phase        string `json:"phase"`
		FailureClass string `json:"failure_class"`
	}{Transport: value.transport, Phase: value.phase, FailureClass: value.failureClass})
	if err != nil {
		return fmt.Errorf("encode authentication audit metadata: %w", err)
	}
	requestID, correlationID := securityRuntimeCorrelation(ctx)
	occurredAt := recorder.clock().UTC()
	if occurredAt.IsZero() {
		return errors.New("security audit clock returned zero time")
	}
	_, err = recorder.store.Append(ctx, Entry{
		ID:                    id,
		SchemaVersion:         SchemaVersion,
		EventCategory:         value.eventCategory,
		OrganizationID:        value.organizationID,
		ActorType:             value.actorType,
		ActorID:               value.actorID,
		Operation:             value.operation,
		AuthorizationDecision: value.decision,
		ScopeType:             securityScopeType(value.organizationID),
		ScopeID:               securityScopeID(value.organizationID),
		Result:                value.result,
		ReasonCode:            value.reasonCode,
		TraceID:               runtimecontext.TraceIDFrom(ctx),
		RequestID:             requestID,
		CorrelationID:         correlationID,
		DiffSummary:           fmt.Sprintf(`{"change":"%s"}`, value.change),
		Metadata:              string(metadata),
		OccurredAt:            occurredAt,
	})
	if err != nil {
		return fmt.Errorf("append security audit: %w", err)
	}
	return nil
}

func securityScopeType(organizationID string) ScopeType {
	if organizationID != "" {
		return ScopeOrganization
	}
	return ScopeSystem
}

func securityScopeID(organizationID string) string { return organizationID }

func securityTransport(ctx context.Context) string {
	if metadata, ok := runtimecontext.MetadataFrom(ctx); ok && metadata.Transport != "" {
		return metadata.Transport
	}
	return "internal"
}

func securityRuntimeCorrelation(ctx context.Context) (string, string) {
	metadata, ok := runtimecontext.MetadataFrom(ctx)
	if !ok {
		return "", ""
	}
	correlationID := metadata.Attributes["correlation_id"]
	if validateIdentifier("security audit correlation ID", correlationID, true) != nil {
		correlationID = ""
	}
	return metadata.RequestID, correlationID
}

func trustedSecurityActor(ctx context.Context) (ActorType, string, string, error) {
	principal, ok := identity.FromContext(ctx)
	if !ok || !principal.Authenticated || principal.TenantID == "" {
		return "", "", "", errors.New("security audit requires a trusted principal")
	}
	if err := validateIdentifier("security audit tenant", principal.TenantID, false); err != nil {
		return "", "", "", err
	}
	switch principal.AuthMethod {
	case identity.AuthMethodJWT:
		if err := validateIdentifier("security audit user", principal.UserID, false); err != nil {
			return "", "", "", errors.New("security audit requires a trusted human actor")
		}
		return ActorHuman, principal.UserID, principal.TenantID, nil
	case identity.AuthMethodServiceToken:
		serviceID, ok := strings.CutPrefix(principal.Subject, "service-account/")
		if !ok || principal.UserID != "" || validateIdentifier("security audit service", serviceID, false) != nil {
			return "", "", "", errors.New("security audit requires a trusted service actor")
		}
		return ActorService, serviceID, principal.TenantID, nil
	case identity.AuthMethodAPIKey:
		return ActorSystem, "development-api-key", principal.TenantID, nil
	default:
		return "", "", "", errors.New("security audit requires a supported principal")
	}
}

func randomSecurityAuditID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
