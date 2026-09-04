// Package configapplication owns the authenticated, audited application
// boundary for immutable organization configuration revisions.
package configapplication

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/configrevision"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/event"
	"github.com/hvritual/yunka.io/framework/event/outbox"
	"github.com/hvritual/yunka.io/framework/execution"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/pkg/operationplan"
)

const correlationIDAttribute = "correlation_id"

const (
	configRevisionEventTopic      = "delivery.configuration"
	configRevisionChangedEvent    = "delivery.configuration-revision.changed"
	configRevisionRolledBackEvent = "delivery.configuration-revision.rolled-back"
)

type Difference struct {
	Path   string
	Change string
}

type RollbackResult struct {
	Revision       configrevision.ConfigRevision
	SourceRevision int64
}

type Operations struct {
	store    configrevision.Store
	audit    audit.Store
	outbox   outbox.TransactionalStore
	executor operation.Executor
	newID    func() (string, error)
	clock    func() time.Time
}

func WithOutbox(store outbox.TransactionalStore) Option {
	return func(operations *Operations) error {
		if store == nil {
			return errors.New("config revision Outbox store is required")
		}
		operations.outbox = store
		return nil
	}
}

type Option func(*Operations) error

func WithIDGenerator(generator func() (string, error)) Option {
	return func(operations *Operations) error {
		if generator == nil {
			return errors.New("config revision ID generator is required")
		}
		operations.newID = generator
		return nil
	}
}

func WithClock(clock func() time.Time) Option {
	return func(operations *Operations) error {
		if clock == nil {
			return errors.New("config revision clock is required")
		}
		operations.clock = clock
		return nil
	}
}

func New(store configrevision.Store, auditStore audit.Store, executor operation.Executor, options ...Option) (*Operations, error) {
	if store == nil || auditStore == nil || executor == nil {
		return nil, errors.New("config revision operations require store, audit store, and executor")
	}
	operations := &Operations{store: store, audit: auditStore, executor: executor, newID: randomID, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("config revision operations option is required")
		}
		if err := option(operations); err != nil {
			return nil, err
		}
	}
	if operations.outbox == nil {
		return nil, errors.New("config revision operations require an Outbox store")
	}
	return operations, nil
}

func (operations *Operations) Change(ctx context.Context, input configrevision.ChangeInput) (configrevision.ConfigRevision, error) {
	if err := operations.ready(); err != nil {
		return configrevision.ConfigRevision{}, err
	}
	value, err := operations.executor.Execute(ctx, changePlan, &input, func(callContext context.Context) (any, error) {
		organizationID, actor, err := trustedActor(callContext)
		if err != nil {
			return nil, err
		}
		var parent configrevision.ConfigRevision
		hasParent := input.ExpectedParentRevision > 0
		if input.ExpectedParentRevision > 0 {
			var readErr error
			parent, readErr = operations.store.ByRevision(callContext, organizationID, input.Kind, input.ConfigKey, input.ExpectedParentRevision)
			if readErr != nil {
				if errors.Is(readErr, configrevision.ErrNotFound) {
					return nil, configrevision.ErrRevisionConflict
				}
				return nil, readErr
			}
		}
		if _, err := decodePayload(input.Payload); err != nil {
			return nil, err
		}
		id, err := operations.newID()
		if err != nil {
			return nil, fmt.Errorf("generate config revision ID: %w", err)
		}
		revision, err := operations.store.Append(callContext, configrevision.AppendInput{ID: id, OrganizationID: organizationID, Kind: input.Kind, ConfigKey: input.ConfigKey, ExpectedParentRevision: input.ExpectedParentRevision, Payload: input.Payload, CreatedByType: actor.revisionType, CreatedByID: actor.id})
		if err != nil {
			return nil, err
		}
		var differences []Difference
		if hasParent {
			differences, err = diff(parent.Payload, revision.Payload)
			if err != nil {
				return nil, err
			}
		} else {
			differences = rootAddedDiff()
		}
		if err := operations.appendAudit(callContext, actor, organizationID, "config.revisions.change", input.Kind, input.ConfigKey, revision.Revision, 0, differences); err != nil {
			return nil, err
		}
		if err := operations.stageOutbox(callContext, configRevisionChangedEvent, revision, 0); err != nil {
			return nil, err
		}
		return revision, nil
	})
	if err != nil {
		return configrevision.ConfigRevision{}, err
	}
	revision, ok := value.(configrevision.ConfigRevision)
	if !ok {
		return configrevision.ConfigRevision{}, errors.New("config revision change returned an unexpected result")
	}
	return revision, nil
}

func (operations *Operations) Compare(ctx context.Context, input configrevision.CompareInput) ([]Difference, error) {
	if err := operations.ready(); err != nil {
		return nil, err
	}
	value, err := operations.executor.Execute(ctx, comparePlan, &input, func(callContext context.Context) (any, error) {
		organizationID, _, err := trustedActor(callContext)
		if err != nil {
			return nil, err
		}
		left, err := operations.store.ByRevision(callContext, organizationID, input.Kind, input.ConfigKey, input.LeftRevision)
		if err != nil {
			return nil, err
		}
		right, err := operations.store.ByRevision(callContext, organizationID, input.Kind, input.ConfigKey, input.RightRevision)
		if err != nil {
			return nil, err
		}
		return diff(left.Payload, right.Payload)
	})
	if err != nil {
		return nil, err
	}
	differences, ok := value.([]Difference)
	if !ok {
		return nil, errors.New("config revision comparison returned an unexpected result")
	}
	return differences, nil
}

func (operations *Operations) Rollback(ctx context.Context, input configrevision.RollbackInput) (RollbackResult, error) {
	if err := operations.ready(); err != nil {
		return RollbackResult{}, err
	}
	value, err := operations.executor.Execute(ctx, rollbackPlan, &input, func(callContext context.Context) (any, error) {
		organizationID, actor, err := trustedActor(callContext)
		if err != nil {
			return nil, err
		}
		source, err := operations.store.ByRevision(callContext, organizationID, input.Kind, input.ConfigKey, input.SourceRevision)
		if err != nil {
			return nil, err
		}
		var parent configrevision.ConfigRevision
		hasParent := input.ExpectedParentRevision > 0
		if input.ExpectedParentRevision > 0 {
			var readErr error
			parent, readErr = operations.store.ByRevision(callContext, organizationID, input.Kind, input.ConfigKey, input.ExpectedParentRevision)
			if readErr != nil {
				if errors.Is(readErr, configrevision.ErrNotFound) {
					return nil, configrevision.ErrRevisionConflict
				}
				return nil, readErr
			}
		}
		id, err := operations.newID()
		if err != nil {
			return nil, fmt.Errorf("generate config revision ID: %w", err)
		}
		revision, err := operations.store.Append(callContext, configrevision.AppendInput{ID: id, OrganizationID: organizationID, Kind: input.Kind, ConfigKey: input.ConfigKey, ExpectedParentRevision: input.ExpectedParentRevision, Payload: source.Payload, CreatedByType: actor.revisionType, CreatedByID: actor.id})
		if err != nil {
			return nil, err
		}
		var differences []Difference
		if hasParent {
			differences, err = diff(parent.Payload, revision.Payload)
			if err != nil {
				return nil, err
			}
		} else {
			differences = rootAddedDiff()
		}
		if err := operations.appendAudit(callContext, actor, organizationID, "config.revisions.rollback", input.Kind, input.ConfigKey, revision.Revision, input.SourceRevision, differences); err != nil {
			return nil, err
		}
		if err := operations.stageOutbox(callContext, configRevisionRolledBackEvent, revision, input.SourceRevision); err != nil {
			return nil, err
		}
		return RollbackResult{Revision: revision, SourceRevision: input.SourceRevision}, nil
	})
	if err != nil {
		return RollbackResult{}, err
	}
	result, ok := value.(RollbackResult)
	if !ok {
		return RollbackResult{}, errors.New("config revision rollback returned an unexpected result")
	}
	return result, nil
}

func (operations *Operations) ready() error {
	if operations == nil || operations.store == nil || operations.audit == nil || operations.outbox == nil || operations.executor == nil || operations.newID == nil || operations.clock == nil {
		return errors.New("config revision operations are not configured")
	}
	return nil
}

var changePlan = plan("config.revisions.change", "config.revisions.write", "local")
var comparePlan = plan("config.revisions.compare", "config.revisions.read", "read_only")
var rollbackPlan = plan("config.revisions.rollback", "config.revisions.rollback", "local")

// ConfigOperationPlans returns a defensive copy of every handwritten
// configuration plan so dictionary validation has no prefix-based exception.
func ConfigOperationPlans() []operationplan.Plan {
	return []operationplan.Plan{clonePlan(changePlan), clonePlan(comparePlan), clonePlan(rollbackPlan)}
}

func clonePlan(plan operationplan.Plan) operationplan.Plan {
	plan.Security.Authentication = slices.Clone(plan.Security.Authentication)
	plan.Security.Permissions = slices.Clone(plan.Security.Permissions)
	plan.Composition.RequiresOperations = slices.Clone(plan.Composition.RequiresOperations)
	plan.Composition.PermissionClosure = slices.Clone(plan.Composition.PermissionClosure)
	plan.ApplicationRequires = slices.Clone(plan.ApplicationRequires)
	plan.Bindings.HTTP = slices.Clone(plan.Bindings.HTTP)
	return plan
}

func plan(operationID, permission, transaction string) operationplan.Plan {
	return operationplan.Plan{OperationID: operationID, Domain: "config", Application: "revisions", UseCase: operationID, Execution: operationplan.Execution{Transaction: transaction, Idempotency: "none"}, Security: operationplan.Security{Authentication: []string{"jwt", "service-token"}, Permissions: []string{permission}, PermissionMode: "all"}, Composition: operationplan.Composition{Boundary: "local"}}
}

type actor struct {
	kind         audit.ActorType
	revisionType configrevision.CreatedByType
	id           string
}

func trustedActor(ctx context.Context) (string, actor, error) {
	principal, ok := identity.FromContext(ctx)
	if !ok || !principal.Authenticated || !canonicalID(principal.TenantID) {
		return "", actor{}, errors.New("config revision requires a trusted principal tenant")
	}
	organizationID := deliveryauthz.OrganizationIDFromContext(ctx)
	if !canonicalID(organizationID) || organizationID != principal.TenantID {
		return "", actor{}, errors.New("config revision requires an authorized organization scope")
	}
	switch principal.AuthMethod {
	case identity.AuthMethodJWT:
		if !canonicalID(principal.UserID) {
			return "", actor{}, errors.New("config revision requires a trusted human actor")
		}
		return organizationID, actor{kind: audit.ActorHuman, revisionType: configrevision.CreatedByHuman, id: principal.UserID}, nil
	case identity.AuthMethodServiceToken:
		id, found := strings.CutPrefix(principal.Subject, "service-account/")
		if !found || !canonicalID(id) || principal.UserID != "" {
			return "", actor{}, errors.New("config revision requires a trusted service actor")
		}
		return organizationID, actor{kind: audit.ActorService, revisionType: configrevision.CreatedByService, id: id}, nil
	default:
		return "", actor{}, errors.New("config revision requires a supported trusted principal")
	}
}

func (operations *Operations) appendAudit(ctx context.Context, actor actor, organizationID, operationID string, kind configrevision.Kind, configKey string, revision, source int64, differences []Difference) error {
	fields := make([]string, 0, len(differences))
	for _, difference := range differences {
		fields = append(fields, auditPath(difference.Path))
	}
	summary, err := audit.BuildDiffSummary("changed", fields)
	if err != nil {
		return fmt.Errorf("encode config revision audit diff: %w", err)
	}
	metadata, present := runtimecontext.MetadataFrom(ctx)
	if !present || metadata.Operation != operationID {
		return errors.New("config revision execution metadata does not match operation")
	}
	id, err := operations.newID()
	if err != nil {
		return fmt.Errorf("generate config audit ID: %w", err)
	}
	occurredAt := operations.clock().UTC()
	if occurredAt.IsZero() {
		return errors.New("config revision clock returned zero time")
	}
	attributes := map[string]any{"kind": kind, "revision": revision}
	if source > 0 {
		attributes["rollback_source_revision"] = source
	}
	if metadata.Transport != "" {
		attributes["transport"] = metadata.Transport
	}
	encoded, err := json.Marshal(attributes)
	if err != nil {
		return errors.New("encode config revision audit metadata")
	}
	reasonCode := "configuration.change.applied"
	if operationID == "config.revisions.rollback" {
		reasonCode = "configuration.rollback.applied"
	}
	_, err = operations.audit.Append(ctx, audit.Entry{ID: id, SchemaVersion: audit.SchemaVersion, EventCategory: audit.EventCategoryConfiguration, OrganizationID: organizationID, ActorType: actor.kind, ActorID: actor.id, Operation: operationID, AuthorizationDecision: audit.DecisionAllowed, ScopeType: audit.ScopeOrganization, ScopeID: organizationID, TargetType: "config.revision", TargetID: configKey, Result: audit.ResultSuccess, ReasonCode: reasonCode, TraceID: runtimecontext.TraceIDFrom(ctx), RequestID: metadata.RequestID, CorrelationID: metadata.Attributes[correlationIDAttribute], DiffSummary: summary, Metadata: string(encoded), OccurredAt: occurredAt})
	if err != nil {
		return fmt.Errorf("record successful config revision audit: %w", err)
	}
	return nil
}

func (operations *Operations) stageOutbox(ctx context.Context, eventType string, revision configrevision.ConfigRevision, rollbackSourceRevision int64) error {
	transaction, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return fmt.Errorf("get configuration transaction handle: %w", err)
	}
	payload := struct {
		OrganizationID         string              `json:"organizationId"`
		Kind                   configrevision.Kind `json:"kind"`
		ConfigKey              string              `json:"configKey"`
		Revision               int64               `json:"revision"`
		RollbackSourceRevision int64               `json:"rollbackSourceRevision,omitzero"`
	}{
		OrganizationID:         revision.OrganizationID,
		Kind:                   revision.Kind,
		ConfigKey:              revision.ConfigKey,
		Revision:               revision.Revision,
		RollbackSourceRevision: rollbackSourceRevision,
	}
	envelope, err := event.NewJSON(configRevisionEventTopic, eventType, "iot-delivery-system/local", payload)
	if err != nil {
		return fmt.Errorf("create configuration Outbox event: %w", err)
	}
	envelope.Subject = revision.ID
	if envelope, err = envelope.Normalize(); err != nil {
		return fmt.Errorf("normalize configuration Outbox event: %w", err)
	}
	if err := operations.outbox.EnqueueTx(ctx, transaction, envelope); err != nil {
		return fmt.Errorf("stage configuration Outbox event: %w", err)
	}
	return nil
}

func diff(leftPayload, rightPayload string) ([]Difference, error) {
	left, err := decodePayload(leftPayload)
	if err != nil {
		return nil, errors.New("decode canonical left config payload")
	}
	right, err := decodePayload(rightPayload)
	if err != nil {
		return nil, errors.New("decode canonical right config payload")
	}
	return diffValues(left, right, ""), nil
}

func decodePayload(payload string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("decode canonical config payload")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode canonical config payload")
	}
	return value, nil
}

func diffValues(left, right any, path string) []Difference {
	result := make([]Difference, 0)
	diffValue(&result, path, left, true, right, true)
	return result
}

func rootAddedDiff() []Difference { return []Difference{{Path: "", Change: "added"}} }

func diffValue(result *[]Difference, path string, left any, leftPresent bool, right any, rightPresent bool) {
	if !leftPresent {
		*result = append(*result, Difference{Path: path, Change: "added"})
		return
	}
	if !rightPresent {
		*result = append(*result, Difference{Path: path, Change: "removed"})
		return
	}
	leftObject, leftIsObject := left.(map[string]any)
	rightObject, rightIsObject := right.(map[string]any)
	if leftIsObject && rightIsObject {
		keys := slices.Collect(maps.Keys(leftObject))
		for key := range rightObject {
			if _, exists := leftObject[key]; !exists {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range keys {
			leftValue, leftOK := leftObject[key]
			rightValue, rightOK := rightObject[key]
			child := path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
			diffValue(result, child, leftValue, leftOK, rightValue, rightOK)
		}
		return
	}
	leftArray, leftIsArray := left.([]any)
	rightArray, rightIsArray := right.([]any)
	if leftIsArray && rightIsArray {
		for index := range max(len(leftArray), len(rightArray)) {
			child := path + "/" + strconv.Itoa(index)
			diffValue(result, child, valueAt(leftArray, index), index < len(leftArray), valueAt(rightArray, index), index < len(rightArray))
		}
		return
	}
	if !jsonEqual(left, right) {
		*result = append(*result, Difference{Path: path, Change: "changed"})
	}
}

func valueAt(values []any, index int) any {
	if index >= len(values) {
		return nil
	}
	return values[index]
}

func jsonEqual(left, right any) bool {
	leftNumber, leftIsNumber := left.(json.Number)
	rightNumber, rightIsNumber := right.(json.Number)
	if leftIsNumber || rightIsNumber {
		if !leftIsNumber || !rightIsNumber {
			return false
		}
		leftValue, leftOK := new(big.Rat).SetString(string(leftNumber))
		rightValue, rightOK := new(big.Rat).SetString(string(rightNumber))
		return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
	}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
func auditPath(path string) string  { return "p_" + base64.RawURLEncoding.EncodeToString([]byte(path)) }
func canonicalID(value string) bool { return value != "" && value == strings.TrimSpace(value) }
func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
