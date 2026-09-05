package backendyunka

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/configapplication"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
	"github.com/hvritual/yunka.io/pkg/operationplan"
)

const permissionDictionaryPath = "contracts/authorization/permission-dictionary.v1.json"

type permissionDictionary struct {
	SchemaVersion            string                   `json:"schemaVersion"`
	DictionaryID             string                   `json:"dictionaryId"`
	DefaultDecision          string                   `json:"defaultDecision"`
	ScopeModel               scopeModel               `json:"scopeModel"`
	Resources                []resourceDefinition     `json:"resources"`
	Permissions              []permissionDefinition   `json:"permissions"`
	Operations               []operationDefinition    `json:"operations"`
	Roles                    []roleDefinition         `json:"roles"`
	Constraints              []constraintDefinition   `json:"constraints"`
	ServiceIdentities        serviceIdentityPolicy    `json:"serviceIdentities"`
	DevelopmentCompatibility developmentCompatibility `json:"developmentCompatibility"`
}

type scopeModel struct {
	Types       []scopeDefinition `json:"types"`
	Inheritance string            `json:"inheritance"`
	DenyWhen    []string          `json:"denyWhen"`
}

type scopeDefinition struct {
	ID     string `json:"id"`
	Format string `json:"format"`
	Parent string `json:"parent,omitempty"`
}

type resourceDefinition struct {
	ID            string   `json:"id"`
	AllowedScopes []string `json:"allowedScopes"`
}

type permissionDefinition struct {
	ID            string   `json:"id"`
	Resource      string   `json:"resource"`
	Action        string   `json:"action"`
	AllowedScopes []string `json:"allowedScopes"`
	Status        string   `json:"status"`
}

type operationDefinition struct {
	ID                 string              `json:"id"`
	Resource           string              `json:"resource"`
	Permission         string              `json:"permission"`
	RequiredScope      string              `json:"requiredScope"`
	RequiresOperations []string            `json:"requiresOperations,omitempty"`
	Risk               string              `json:"risk"`
	Writes             bool                `json:"writes"`
	Transports         operationTransports `json:"transports"`
}

type operationTransports struct {
	GRPC string          `json:"grpc"`
	REST []transportPath `json:"rest"`
	MCP  []string        `json:"mcp"`
}

type transportPath struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type roleDefinition struct {
	ID           string      `json:"id"`
	BindingScope string      `json:"bindingScope"`
	Grants       []roleGrant `json:"grants"`
}

type roleGrant struct {
	Permission    string   `json:"permission"`
	AllowedScopes []string `json:"allowedScopes"`
}

type constraintDefinition struct {
	ID                string   `json:"id"`
	EnforcementStatus string   `json:"enforcementStatus"`
	AppliesTo         []string `json:"appliesTo"`
	Rule              string   `json:"rule"`
}

type serviceIdentityPolicy struct {
	DefaultGrants []string `json:"defaultGrants"`
	RequiredScope string   `json:"requiredScope"`
	Rule          string   `json:"rule"`
}

type developmentCompatibility struct {
	Status                           string                  `json:"status"`
	Rule                             string                  `json:"rule"`
	LocalRoleProfiles                []localRoleProfile      `json:"localRoleProfiles"`
	LegacyExtensionPermissionAliases []legacyPermissionAlias `json:"legacyExtensionPermissionAliases"`
}

type localRoleProfile struct {
	LocalRole   string   `json:"localRole"`
	Permissions []string `json:"permissions"`
}

type legacyPermissionAlias struct {
	LegacyPermission string `json:"legacyPermission"`
	Replacement      string `json:"replacement"`
	Reason           string `json:"reason"`
}

func TestPermissionDictionaryIsCompleteAndMatchesGeneratedOperations(t *testing.T) {
	dictionary := loadPermissionDictionary(t)
	plans := loadGeneratedOperationPlans(t)
	if err := validatePermissionDictionary(dictionary, plans); err != nil {
		t.Fatalf("permission dictionary validation failed: %v", err)
	}
}

func TestPermissionDictionaryIntegrityGateRejectsMissingRequiredMappings(t *testing.T) {
	dictionary := loadPermissionDictionary(t)
	plans := loadGeneratedOperationPlans(t)

	for name, mutate := range map[string]func(*permissionDictionary){
		"operation":  func(value *permissionDictionary) { value.Operations = value.Operations[1:] },
		"permission": func(value *permissionDictionary) { value.Permissions = value.Permissions[1:] },
		"role":       func(value *permissionDictionary) { value.Roles = value.Roles[1:] },
		"scope":      func(value *permissionDictionary) { value.ScopeModel.Types = value.ScopeModel.Types[1:] },
		"extra resource": func(value *permissionDictionary) {
			value.Resources = append(value.Resources, resourceDefinition{ID: "delivery.unused", AllowedScopes: []string{"project"}})
		},
		"expanded resource scope": func(value *permissionDictionary) {
			value.Resources[1].AllowedScopes = append(value.Resources[1].AllowedScopes, "organization")
		},
		"role grant":  func(value *permissionDictionary) { value.Roles[0].Grants = value.Roles[0].Grants[1:] },
		"inheritance": func(value *permissionDictionary) { value.ScopeModel.Inheritance = "" },
		"REST trace":  func(value *permissionDictionary) { value.Operations[0].Transports.REST = nil },
		"MCP trace":   func(value *permissionDictionary) { value.Operations[2].Transports.MCP = nil },
		"wildcard":    func(value *permissionDictionary) { value.Permissions[0].Action = "*" },
		"viewer extra write": func(value *permissionDictionary) {
			value.Roles[4].Grants = append(value.Roles[4].Grants, roleGrant{Permission: "delivery.work-items.update", AllowedScopes: []string{"object"}})
		},
		"release approver extra role binding": func(value *permissionDictionary) {
			value.Roles[2].Grants = append(value.Roles[2].Grants, roleGrant{Permission: "identity.role-bindings.manage", AllowedScopes: []string{"project"}})
		},
		"project administrator organization create": func(value *permissionDictionary) {
			value.Roles[1].Grants = append(value.Roles[1].Grants, roleGrant{Permission: "delivery.projects.create", AllowedScopes: []string{"organization"}})
		},
		"viewer expanded scope": func(value *permissionDictionary) {
			value.Roles[4].Grants[0].AllowedScopes = append(value.Roles[4].Grants[0].AllowedScopes, "organization")
		},
		"operation permission resource mismatch": func(value *permissionDictionary) { value.Operations[0].Resource = "delivery.work-items" },
		"permission resource scope mismatch": func(value *permissionDictionary) {
			value.Permissions[1].AllowedScopes = append(value.Permissions[1].AllowedScopes, "organization")
		},
		"unknown risk":      func(value *permissionDictionary) { value.Operations[0].Risk = "critical" },
		"high risk lowered": func(value *permissionDictionary) { value.Operations[6].Risk = "low" },
		"required scope changed within permission": func(value *permissionDictionary) {
			value.Operations[1].RequiredScope = "object"
		},
		"unknown constraint permission": func(value *permissionDictionary) {
			value.Constraints[0].AppliesTo = append(value.Constraints[0].AppliesTo, "delivery.unknown")
		},
		"constraint rule":           func(value *permissionDictionary) { value.Constraints[0].Rule = "" },
		"service identity rule":     func(value *permissionDictionary) { value.ServiceIdentities.Rule = "" },
		"development compatibility": func(value *permissionDictionary) { value.DevelopmentCompatibility.Status = "" },
		"local profile drift": func(value *permissionDictionary) {
			value.DevelopmentCompatibility.LocalRoleProfiles[0].Permissions = value.DevelopmentCompatibility.LocalRoleProfiles[0].Permissions[1:]
		},
		"local alias drift": func(value *permissionDictionary) {
			value.DevelopmentCompatibility.LegacyExtensionPermissionAliases[0].Replacement = "delivery.work-items.create"
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := clonePermissionDictionary(t, dictionary)
			mutate(&candidate)
			if err := validatePermissionDictionary(candidate, plans); err == nil {
				t.Fatal("integrity gate accepted a dictionary with a missing required mapping")
			}
		})
	}
}

func TestPermissionDictionaryStrictDecodeRejectsUnknownAndMissingContractFields(t *testing.T) {
	contents, err := os.ReadFile(permissionDictionaryPath)
	if err != nil {
		t.Fatalf("read permission dictionary: %v", err)
	}
	plans := loadGeneratedOperationPlans(t)
	for name, mutate := range map[string]func(map[string]any){
		"unknown field": func(value map[string]any) { value["unexpected"] = true },
		"misspelled development compatibility": func(value map[string]any) {
			value["developmentCompatiblity"] = value["developmentCompatibility"]
			delete(value, "developmentCompatibility")
		},
		"missing constraint rule":       func(value map[string]any) { delete(value["constraints"].([]any)[0].(map[string]any), "rule") },
		"missing service identity rule": func(value map[string]any) { delete(value["serviceIdentities"].(map[string]any), "rule") },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := mutateRawDictionary(t, contents, mutate)
			dictionary, err := decodePermissionDictionary(candidate)
			if err == nil {
				err = validatePermissionDictionary(dictionary, plans)
			}
			if err == nil {
				t.Fatal("integrity gate accepted an unknown or missing contract field")
			}
		})
	}
	if _, err := decodePermissionDictionary(append(contents, []byte("\n{}")...)); err == nil {
		t.Fatal("strict decoder accepted multiple JSON values")
	}
}

func TestDevelopmentOnlyLocalProfilesExactlyMatchLocalAuth(t *testing.T) {
	dictionary := loadPermissionDictionary(t)
	profiles := dictionary.DevelopmentCompatibility.LocalRoleProfiles
	if len(profiles) != 4 {
		t.Fatalf("development local profile count = %d, want 4", len(profiles))
	}
	resolver := localauth.NewGrantResolver()
	for _, profile := range profiles {
		requested := localauth.DevelopmentPermissionsForRole(profile.LocalRole)
		if len(requested) == 0 {
			t.Fatalf("local profile %q is not implemented by localauth", profile.LocalRole)
		}
		grants, err := resolver.ResolveGrants(context.Background(), authz.GrantRequest{Principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, Roles: []string{profile.LocalRole}}, Permissions: requested})
		if err != nil {
			t.Fatalf("resolve local profile %q: %v", profile.LocalRole, err)
		}
		actual := make([]string, 0, len(grants))
		for _, grant := range grants {
			actual = append(actual, string(grant.Permission))
		}
		slices.Sort(actual)
		expected := slices.Clone(profile.Permissions)
		slices.Sort(expected)
		requestedStrings := make([]string, 0, len(requested))
		for _, permission := range requested {
			requestedStrings = append(requestedStrings, string(permission))
		}
		slices.Sort(requestedStrings)
		if !slices.Equal(requestedStrings, expected) {
			t.Fatalf("local profile %q source permissions = %#v, want %#v", profile.LocalRole, requestedStrings, expected)
		}
		if !slices.Equal(actual, expected) {
			t.Fatalf("local profile %q grants = %#v, want %#v", profile.LocalRole, actual, expected)
		}
	}
}

func mutateRawDictionary(t *testing.T, contents []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(contents, &value); err != nil {
		t.Fatalf("decode raw dictionary mutation: %v", err)
	}
	mutate(value)
	result, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode raw dictionary mutation: %v", err)
	}
	return result
}

func loadPermissionDictionary(t *testing.T) permissionDictionary {
	t.Helper()
	contents, err := os.ReadFile(permissionDictionaryPath)
	if err != nil {
		t.Fatalf("read permission dictionary: %v", err)
	}
	dictionary, err := decodePermissionDictionary(contents)
	if err != nil {
		t.Fatalf("decode permission dictionary: %v", err)
	}
	return dictionary
}

func loadGeneratedOperationPlans(t *testing.T) operationplan.Set {
	t.Helper()
	contents, err := os.ReadFile("contracts/generated/operation-plans.json")
	if err != nil {
		t.Fatalf("read generated operation plans: %v", err)
	}
	var plans operationplan.Set
	if err := json.Unmarshal(contents, &plans); err != nil {
		t.Fatalf("decode generated operation plans: %v", err)
	}
	return plans
}

func clonePermissionDictionary(t *testing.T, dictionary permissionDictionary) permissionDictionary {
	t.Helper()
	contents, err := json.Marshal(dictionary)
	if err != nil {
		t.Fatalf("encode dictionary clone: %v", err)
	}
	clone, err := decodePermissionDictionary(contents)
	if err != nil {
		t.Fatalf("decode dictionary clone: %v", err)
	}
	return clone
}

func decodePermissionDictionary(contents []byte) (permissionDictionary, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var dictionary permissionDictionary
	if err := decoder.Decode(&dictionary); err != nil {
		return permissionDictionary{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return permissionDictionary{}, fmt.Errorf("permission dictionary contains multiple JSON values")
		}
		return permissionDictionary{}, err
	}
	return dictionary, nil
}

func validatePermissionDictionary(dictionary permissionDictionary, plans operationplan.Set) error {
	if dictionary.DictionaryID != "iot-delivery.permission-dictionary" {
		return fmt.Errorf("dictionaryId = %q", dictionary.DictionaryID)
	}
	if !regexp.MustCompile(`^1\.0\.0$`).MatchString(dictionary.SchemaVersion) {
		return fmt.Errorf("schemaVersion = %q, want 1.0.0", dictionary.SchemaVersion)
	}
	if dictionary.DefaultDecision != "deny" {
		return fmt.Errorf("defaultDecision = %q, want deny", dictionary.DefaultDecision)
	}

	scopes, err := indexScopes(dictionary.ScopeModel.Types)
	if err != nil {
		return err
	}
	if err := requireScopeModel(dictionary.ScopeModel, scopes); err != nil {
		return err
	}
	resources, err := indexResources(dictionary.Resources, scopes)
	if err != nil {
		return err
	}
	permissions, err := indexPermissions(dictionary.Permissions, resources, scopes)
	if err != nil {
		return err
	}
	if err := validateOperations(dictionary.Operations, plans, resources, permissions, scopes); err != nil {
		return err
	}
	if err := validateRoles(dictionary.Roles, permissions, scopes); err != nil {
		return err
	}
	if err := validateConstraints(dictionary.Constraints, permissions); err != nil {
		return err
	}
	if err := validateServiceIdentities(dictionary.ServiceIdentities); err != nil {
		return err
	}
	if err := validateDevelopmentCompatibility(dictionary.DevelopmentCompatibility); err != nil {
		return err
	}
	return nil
}

func indexScopes(definitions []scopeDefinition) (map[string]scopeDefinition, error) {
	result := make(map[string]scopeDefinition, len(definitions))
	for _, definition := range definitions {
		if err := validateID("scope", definition.ID); err != nil {
			return nil, err
		}
		if definition.Format == "" || strings.Contains(definition.Format, "*") {
			return nil, fmt.Errorf("scope %q has an invalid format", definition.ID)
		}
		if _, exists := result[definition.ID]; exists {
			return nil, fmt.Errorf("duplicate scope %q", definition.ID)
		}
		result[definition.ID] = definition
	}
	return result, nil
}

func requireScopeModel(model scopeModel, scopes map[string]scopeDefinition) error {
	const inheritance = "A binding may be evaluated only from organization to an owned project to an owned object; it never inherits upward or across organizations."
	if model.Inheritance != inheritance {
		return fmt.Errorf("scope inheritance direction is missing or invalid")
	}
	wantDeny := []string{"scope is absent", "scope type is unknown", "resource ownership is unknown", "cross-organization access", "operation is unregistered", "permission is unregistered"}
	if !slices.Equal(model.DenyWhen, wantDeny) {
		return fmt.Errorf("scope default-deny rules are incomplete")
	}
	want := map[string]scopeDefinition{
		"organization": {ID: "organization", Format: "organization:{organization_id}"},
		"project":      {ID: "project", Format: "project:{project_id}", Parent: "organization"},
		"object":       {ID: "object", Format: "object:{resource_type}:{object_id}", Parent: "project"},
	}
	if len(scopes) != len(want) {
		return fmt.Errorf("scope count = %d, want %d", len(scopes), len(want))
	}
	for id, expected := range want {
		actual, ok := scopes[id]
		if !ok || actual != expected {
			return fmt.Errorf("scope %q = %#v, want %#v", id, actual, expected)
		}
	}
	return nil
}

type permissionContract struct {
	resource string
	action   string
	status   string
	scopes   []string
}

var expectedPermissions = map[string]permissionContract{
	"delivery.dashboard.read":            {resource: "delivery.dashboard", action: "read", status: "active", scopes: []string{"organization"}},
	"delivery.work-items.read":           {resource: "delivery.work-items", action: "read", status: "active", scopes: []string{"project", "object"}},
	"delivery.work-items.create":         {resource: "delivery.work-items", action: "create", status: "active", scopes: []string{"project"}},
	"delivery.work-items.update":         {resource: "delivery.work-items", action: "update", status: "active", scopes: []string{"object"}},
	"delivery.work-items.comment.create": {resource: "delivery.work-items", action: "comment.create", status: "active", scopes: []string{"object"}},
	"delivery.work-items.context.update": {resource: "delivery.work-items", action: "context.update", status: "active", scopes: []string{"object"}},
	"delivery.work-items.gate.advance":   {resource: "delivery.work-items", action: "gate.advance", status: "active", scopes: []string{"object"}},
	"delivery.work-items.close":          {resource: "delivery.work-items", action: "close", status: "active", scopes: []string{"object"}},
	"delivery.projects.create":           {resource: "delivery.projects", action: "create", status: "active", scopes: []string{"organization"}},
	"delivery.projects.read":             {resource: "delivery.projects", action: "read", status: "active", scopes: []string{"project"}},
	"delivery.releases.read":             {resource: "delivery.releases", action: "read", status: "active", scopes: []string{"project"}},
	"delivery.sprints.read":              {resource: "delivery.sprints", action: "read", status: "active", scopes: []string{"project"}},
	"delivery.milestones.read":           {resource: "delivery.milestones", action: "read", status: "active", scopes: []string{"project"}},
	"delivery.releases.create":           {resource: "delivery.releases", action: "create", status: "active", scopes: []string{"project"}},
	"delivery.sprints.create":            {resource: "delivery.sprints", action: "create", status: "active", scopes: []string{"project"}},
	"delivery.milestones.create":         {resource: "delivery.milestones", action: "create", status: "active", scopes: []string{"project"}},
	"identity.teams.manage":              {resource: "identity.teams", action: "manage", status: "reserved", scopes: []string{"organization", "project"}},
	"identity.memberships.manage":        {resource: "identity.memberships", action: "manage", status: "reserved", scopes: []string{"project"}},
	"identity.roles.manage":              {resource: "identity.roles", action: "manage", status: "reserved", scopes: []string{"organization"}},
	"identity.role-bindings.manage":      {resource: "identity.role-bindings", action: "manage", status: "reserved", scopes: []string{"project"}},
	"audit.events.read":                  {resource: "audit.events", action: "read", status: "reserved", scopes: []string{"organization", "project", "object"}},
	"config.revisions.write":             {resource: "config.revisions", action: "write", status: "active", scopes: []string{"organization"}},
	"config.revisions.read":              {resource: "config.revisions", action: "read", status: "active", scopes: []string{"organization"}},
	"config.revisions.rollback":          {resource: "config.revisions", action: "rollback", status: "active", scopes: []string{"organization"}},
}

var expectedResources = map[string][]string{
	"delivery.dashboard":     {"organization"},
	"delivery.work-items":    {"project", "object"},
	"delivery.projects":      {"organization", "project"},
	"delivery.releases":      {"project"},
	"delivery.sprints":       {"project"},
	"delivery.milestones":    {"project"},
	"identity.teams":         {"organization", "project"},
	"identity.memberships":   {"project"},
	"identity.roles":         {"organization"},
	"identity.role-bindings": {"project"},
	"audit.events":           {"organization", "project", "object"},
	"config.revisions":       {"organization"},
}

func indexResources(definitions []resourceDefinition, scopes map[string]scopeDefinition) (map[string]resourceDefinition, error) {
	if len(definitions) != len(expectedResources) {
		return nil, fmt.Errorf("resource count = %d, want %d", len(definitions), len(expectedResources))
	}
	result := make(map[string]resourceDefinition, len(definitions))
	for _, definition := range definitions {
		if err := validateID("resource", definition.ID); err != nil {
			return nil, err
		}
		if err := validateScopes("resource "+definition.ID, definition.AllowedScopes, scopes); err != nil {
			return nil, err
		}
		expected, exists := expectedResources[definition.ID]
		if !exists || !slices.Equal(definition.AllowedScopes, expected) {
			return nil, fmt.Errorf("resource %q does not match the versioned resource contract", definition.ID)
		}
		if _, exists := result[definition.ID]; exists {
			return nil, fmt.Errorf("duplicate resource %q", definition.ID)
		}
		result[definition.ID] = definition
	}
	return result, nil
}

func indexPermissions(definitions []permissionDefinition, resources map[string]resourceDefinition, scopes map[string]scopeDefinition) (map[string]permissionDefinition, error) {
	if len(definitions) != len(expectedPermissions) {
		return nil, fmt.Errorf("permission count = %d, want %d", len(definitions), len(expectedPermissions))
	}
	result := make(map[string]permissionDefinition, len(definitions))
	for _, definition := range definitions {
		if err := validateID("permission", definition.ID); err != nil {
			return nil, err
		}
		resource, exists := resources[definition.Resource]
		if !exists {
			return nil, fmt.Errorf("permission %q references unknown resource %q", definition.ID, definition.Resource)
		}
		if definition.Action == "" || strings.Contains(definition.Action, "*") {
			return nil, fmt.Errorf("permission %q has invalid action", definition.ID)
		}
		if definition.Status != "active" && definition.Status != "reserved" {
			return nil, fmt.Errorf("permission %q has invalid status %q", definition.ID, definition.Status)
		}
		if err := validateScopes("permission "+definition.ID, definition.AllowedScopes, scopes); err != nil {
			return nil, err
		}
		for _, scope := range definition.AllowedScopes {
			if !slices.Contains(resource.AllowedScopes, scope) {
				return nil, fmt.Errorf("permission %q grants scope %q outside resource %q", definition.ID, scope, definition.Resource)
			}
		}
		expected, exists := expectedPermissions[definition.ID]
		if !exists || expected.resource != definition.Resource || expected.action != definition.Action || expected.status != definition.Status || !slices.Equal(expected.scopes, definition.AllowedScopes) {
			return nil, fmt.Errorf("permission %q does not match the versioned permission contract", definition.ID)
		}
		if _, exists := result[definition.ID]; exists {
			return nil, fmt.Errorf("duplicate permission %q", definition.ID)
		}
		result[definition.ID] = definition
	}
	return result, nil
}

type operationContract struct {
	resource           string
	permission         string
	requiredScope      string
	requiresOperations []string
	risk               string
	writes             bool
}

var expectedOperations = map[string]operationContract{
	"delivery.dashboard.get":        {resource: "delivery.dashboard", permission: "delivery.dashboard.read", requiredScope: "organization", risk: "low", writes: false},
	"delivery.items.list":           {resource: "delivery.work-items", permission: "delivery.work-items.read", requiredScope: "project", risk: "low", writes: false},
	"delivery.items.get":            {resource: "delivery.work-items", permission: "delivery.work-items.read", requiredScope: "object", risk: "low", writes: false},
	"delivery.items.search":         {resource: "delivery.work-items", permission: "delivery.work-items.read", requiredScope: "project", risk: "low", writes: false},
	"delivery.items.similarity":     {resource: "delivery.work-items", permission: "delivery.work-items.read", requiredScope: "project", risk: "low", writes: false},
	"delivery.items.create":         {resource: "delivery.work-items", permission: "delivery.work-items.create", requiredScope: "project", risk: "medium", writes: true},
	"delivery.items.update":         {resource: "delivery.work-items", permission: "delivery.work-items.update", requiredScope: "object", requiresOperations: []string{"delivery.items.update-context"}, risk: "medium", writes: true},
	"delivery.items.comment.create": {resource: "delivery.work-items", permission: "delivery.work-items.comment.create", requiredScope: "object", risk: "low", writes: true},
	"delivery.items.update-context": {resource: "delivery.work-items", permission: "delivery.work-items.context.update", requiredScope: "object", risk: "medium", writes: true},
	"delivery.items.advance-gate":   {resource: "delivery.work-items", permission: "delivery.work-items.gate.advance", requiredScope: "object", risk: "high", writes: true},
	"delivery.items.close":          {resource: "delivery.work-items", permission: "delivery.work-items.close", requiredScope: "object", risk: "high", writes: true},
	"delivery.projects.create":      {resource: "delivery.projects", permission: "delivery.projects.create", requiredScope: "organization", risk: "high", writes: true},
	"delivery.projects.list":        {resource: "delivery.projects", permission: "delivery.projects.read", requiredScope: "project", risk: "low", writes: false},
	"delivery.releases.create":      {resource: "delivery.releases", permission: "delivery.releases.create", requiredScope: "project", risk: "high", writes: true},
	"delivery.releases.list":        {resource: "delivery.releases", permission: "delivery.releases.read", requiredScope: "project", risk: "low", writes: false},
	"delivery.sprints.create":       {resource: "delivery.sprints", permission: "delivery.sprints.create", requiredScope: "project", risk: "medium", writes: true},
	"delivery.sprints.list":         {resource: "delivery.sprints", permission: "delivery.sprints.read", requiredScope: "project", risk: "low", writes: false},
	"delivery.milestones.create":    {resource: "delivery.milestones", permission: "delivery.milestones.create", requiredScope: "project", risk: "medium", writes: true},
	"delivery.milestones.list":      {resource: "delivery.milestones", permission: "delivery.milestones.read", requiredScope: "project", risk: "low", writes: false},
	"config.revisions.change":       {resource: "config.revisions", permission: "config.revisions.write", requiredScope: "organization", risk: "high", writes: true},
	"config.revisions.compare":      {resource: "config.revisions", permission: "config.revisions.read", requiredScope: "organization", risk: "low", writes: false},
	"config.revisions.rollback":     {resource: "config.revisions", permission: "config.revisions.rollback", requiredScope: "organization", risk: "high", writes: true},
}

func validateOperations(definitions []operationDefinition, plans operationplan.Set, resources map[string]resourceDefinition, permissions map[string]permissionDefinition, scopes map[string]scopeDefinition) error {
	plans.Operations = append(slices.Clone(plans.Operations), configapplication.ConfigOperationPlans()...)
	if len(definitions) != len(plans.Operations) || len(definitions) != len(expectedOperations) {
		return fmt.Errorf("dictionary operation count = %d, generated operation count = %d, expected operation count = %d", len(definitions), len(plans.Operations), len(expectedOperations))
	}
	plansByID := make(map[string]operationplan.Plan, len(plans.Operations))
	for _, plan := range plans.Operations {
		plansByID[plan.OperationID] = plan
	}
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if err := validateID("operation", definition.ID); err != nil {
			return err
		}
		if _, exists := seen[definition.ID]; exists {
			return fmt.Errorf("duplicate operation %q", definition.ID)
		}
		seen[definition.ID] = struct{}{}
		plan, exists := plansByID[definition.ID]
		if !exists {
			return fmt.Errorf("dictionary operation %q is not generated", definition.ID)
		}
		expected, exists := expectedOperations[definition.ID]
		if !exists || definition.Resource != expected.resource || definition.Permission != expected.permission || definition.RequiredScope != expected.requiredScope || !slices.Equal(definition.RequiresOperations, expected.requiresOperations) || definition.Risk != expected.risk || definition.Writes != expected.writes {
			return fmt.Errorf("operation %q does not match the versioned operation contract", definition.ID)
		}
		if _, exists := resources[definition.Resource]; !exists {
			return fmt.Errorf("operation %q references unknown resource %q", definition.ID, definition.Resource)
		}
		permission, exists := permissions[definition.Permission]
		if !exists || permission.Status != "active" {
			return fmt.Errorf("operation %q references unavailable permission %q", definition.ID, definition.Permission)
		}
		expectedPlanPermissions := []string{definition.Permission}
		for _, requiredID := range definition.RequiresOperations {
			required, exists := expectedOperations[requiredID]
			if !exists || required.requiredScope != definition.RequiredScope {
				return fmt.Errorf("operation %q has unsupported required operation %q", definition.ID, requiredID)
			}
			expectedPlanPermissions = append(expectedPlanPermissions, required.permission)
		}
		slices.Sort(expectedPlanPermissions)
		if !slices.Equal(plan.Security.Permissions, expectedPlanPermissions) || !slices.Equal(plan.Composition.RequiresOperations, definition.RequiresOperations) {
			return fmt.Errorf("operation %q permission does not match generated plan", definition.ID)
		}
		if strings.HasPrefix(definition.ID, "config.revisions.") && (plan.Security.PermissionMode != "all" || !slices.Equal(plan.Security.Authentication, []string{"jwt", "service-token"}) || plan.Execution.Idempotency != "none" || plan.Composition.Boundary != "local" || plan.Bindings.RPC != "" || len(plan.Bindings.HTTP) != 0) {
			return fmt.Errorf("configuration operation %q does not match the handwritten plan contract", definition.ID)
		}
		if definition.Resource != permission.Resource {
			return fmt.Errorf("operation %q resource %q does not match permission resource %q", definition.ID, definition.Resource, permission.Resource)
		}
		if _, exists := scopes[definition.RequiredScope]; !exists || !slices.Contains(permission.AllowedScopes, definition.RequiredScope) {
			return fmt.Errorf("operation %q has invalid required scope %q", definition.ID, definition.RequiredScope)
		}
		if !slices.Contains([]string{"low", "medium", "high"}, definition.Risk) {
			return fmt.Errorf("operation %q is missing risk or gRPC transport", definition.ID)
		}
		if definition.Transports.GRPC != plan.Bindings.RPC {
			return fmt.Errorf("operation %q gRPC transport does not match generated plan", definition.ID)
		}
		if definition.Transports.GRPC != "" {
			if err := validateTransportTrace(definition); err != nil {
				return err
			}
		} else if definition.Transports.GRPC != "" || len(definition.Transports.REST) != 0 || len(definition.Transports.MCP) != 0 {
			return fmt.Errorf("manual configuration operation %q unexpectedly declares a transport", definition.ID)
		}
		if definition.Writes != (plan.Execution.Transaction != "read_only") {
			return fmt.Errorf("operation %q write flag does not match generated transaction", definition.ID)
		}
	}
	return nil
}

func validateTransportTrace(definition operationDefinition) error {
	type trace struct {
		rest []transportPath
		mcp  []string
	}
	want := map[string]trace{
		"delivery.dashboard.get":        {rest: []transportPath{{Method: "GET", Path: "/api/dashboard"}}},
		"delivery.items.list":           {},
		"delivery.items.get":            {rest: []transportPath{{Method: "GET", Path: "/api/items/{item_id}"}}, mcp: []string{"delivery.get_work_item"}},
		"delivery.items.search":         {rest: []transportPath{{Method: "GET", Path: "/api/items"}}, mcp: []string{"delivery.list_work_items"}},
		"delivery.items.similarity":     {rest: []transportPath{{Method: "POST", Path: "/api/items/similarity"}}, mcp: []string{"delivery.find_similar"}},
		"delivery.items.create":         {rest: []transportPath{{Method: "POST", Path: "/api/items"}}, mcp: []string{"delivery.create_work_item"}},
		"delivery.items.update":         {rest: []transportPath{{Method: "PATCH", Path: "/api/items/{item_id}"}}, mcp: []string{"delivery.update_work_item"}},
		"delivery.items.comment.create": {rest: []transportPath{{Method: "POST", Path: "/api/items/{item_id}/comments"}}, mcp: []string{"delivery.add_comment"}},
		"delivery.items.update-context": {rest: []transportPath{{Method: "PATCH", Path: "/api/items/{item_id}"}}},
		"delivery.items.advance-gate":   {rest: []transportPath{{Method: "POST", Path: "/api/items/{item_id}/gates/{gate}"}}, mcp: []string{"delivery.advance_gate"}},
		"delivery.items.close":          {rest: []transportPath{{Method: "POST", Path: "/api/items/{item_id}/close"}}, mcp: []string{"delivery.close_work_item"}},
		"delivery.projects.create":      {rest: []transportPath{{Method: "POST", Path: "/api/projects"}}, mcp: []string{"delivery.create_project"}},
		"delivery.projects.list":        {rest: []transportPath{{Method: "GET", Path: "/api/projects"}}, mcp: []string{"delivery.list_projects"}},
		"delivery.releases.create":      {rest: []transportPath{{Method: "POST", Path: "/api/releases"}}, mcp: []string{"delivery.create_release"}},
		"delivery.releases.list":        {rest: []transportPath{{Method: "GET", Path: "/api/releases"}}, mcp: []string{"delivery.list_releases"}},
		"delivery.sprints.create":       {rest: []transportPath{{Method: "POST", Path: "/api/sprints"}}, mcp: []string{"delivery.create_sprint"}},
		"delivery.sprints.list":         {rest: []transportPath{{Method: "GET", Path: "/api/sprints"}}, mcp: []string{"delivery.list_sprints"}},
		"delivery.milestones.create":    {rest: []transportPath{{Method: "POST", Path: "/api/milestones"}}, mcp: []string{"delivery.create_milestone"}},
		"delivery.milestones.list":      {rest: []transportPath{{Method: "GET", Path: "/api/milestones"}}, mcp: []string{"delivery.list_milestones"}},
	}
	expected, exists := want[definition.ID]
	if !exists || !slices.Equal(definition.Transports.REST, expected.rest) || !slices.Equal(definition.Transports.MCP, expected.mcp) {
		return fmt.Errorf("operation %q transport trace is incomplete or incorrect", definition.ID)
	}
	return nil
}

type roleContract struct {
	bindingScope string
	permissions  []string
}

var expectedRoles = map[string]roleContract{
	"system-administrator":  {bindingScope: "organization", permissions: []string{"delivery.dashboard.read", "delivery.work-items.read", "delivery.work-items.create", "delivery.work-items.update", "delivery.work-items.comment.create", "delivery.work-items.context.update", "delivery.work-items.gate.advance", "delivery.work-items.close", "delivery.projects.create", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read", "delivery.releases.create", "delivery.sprints.create", "delivery.milestones.create", "identity.teams.manage", "identity.memberships.manage", "identity.roles.manage", "identity.role-bindings.manage", "audit.events.read", "config.revisions.write", "config.revisions.read", "config.revisions.rollback"}},
	"project-administrator": {bindingScope: "project", permissions: []string{"delivery.work-items.read", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read", "delivery.work-items.create", "delivery.work-items.update", "delivery.work-items.comment.create", "delivery.work-items.context.update", "delivery.work-items.gate.advance", "delivery.work-items.close", "delivery.releases.create", "delivery.sprints.create", "delivery.milestones.create", "identity.memberships.manage", "identity.role-bindings.manage"}},
	"release-approver":      {bindingScope: "project", permissions: []string{"delivery.work-items.read", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read", "delivery.work-items.gate.advance", "delivery.work-items.close"}},
	"contributor":           {bindingScope: "project", permissions: []string{"delivery.work-items.read", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read", "delivery.work-items.create", "delivery.work-items.update", "delivery.work-items.comment.create", "delivery.work-items.context.update"}},
	"viewer":                {bindingScope: "project", permissions: []string{"delivery.work-items.read", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read"}},
	"auditor":               {bindingScope: "organization", permissions: []string{"delivery.dashboard.read", "delivery.work-items.read", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read", "audit.events.read"}},
}

func validateRoles(definitions []roleDefinition, permissions map[string]permissionDefinition, scopes map[string]scopeDefinition) error {
	if len(definitions) != len(expectedRoles) {
		return fmt.Errorf("role count = %d, want %d", len(definitions), len(expectedRoles))
	}
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		expected, exists := expectedRoles[definition.ID]
		if !exists || definition.BindingScope != expected.bindingScope {
			return fmt.Errorf("role %q has invalid binding scope %q", definition.ID, definition.BindingScope)
		}
		if _, exists := seen[definition.ID]; exists || len(definition.Grants) != len(expected.permissions) {
			return fmt.Errorf("role %q does not have its exact grant count", definition.ID)
		}
		seen[definition.ID] = struct{}{}
		granted := make(map[string]roleGrant, len(definition.Grants))
		for _, grant := range definition.Grants {
			permission, exists := permissions[grant.Permission]
			if !exists {
				return fmt.Errorf("role %q references unknown permission %q", definition.ID, grant.Permission)
			}
			if _, exists := granted[grant.Permission]; exists {
				return fmt.Errorf("role %q duplicates permission %q", definition.ID, grant.Permission)
			}
			if err := validateScopes("role "+definition.ID+" grant "+grant.Permission, grant.AllowedScopes, scopes); err != nil {
				return err
			}
			if !slices.Equal(grant.AllowedScopes, expectedPermissions[grant.Permission].scopes) || !slices.Equal(grant.AllowedScopes, permission.AllowedScopes) {
				return fmt.Errorf("role %q grant %q has an expanded or reduced scope", definition.ID, grant.Permission)
			}
			granted[grant.Permission] = grant
		}
		for _, permission := range expected.permissions {
			if _, exists := granted[permission]; !exists {
				return fmt.Errorf("role %q is missing required permission %q", definition.ID, permission)
			}
		}
	}
	return nil
}

func validateConstraints(definitions []constraintDefinition, permissions map[string]permissionDefinition) error {
	want := constraintDefinition{ID: "no-self-production-verification", EnforcementStatus: "enforced-s0-03-07", AppliesTo: []string{"delivery.work-items.gate.advance", "delivery.work-items.close"}, Rule: "An implementer must not production-verify or close their own change."}
	if len(definitions) != 1 || definitions[0].ID != want.ID || definitions[0].EnforcementStatus != want.EnforcementStatus || definitions[0].Rule != want.Rule || !slices.Equal(definitions[0].AppliesTo, want.AppliesTo) {
		return fmt.Errorf("segregation-of-duties constraint is incomplete")
	}
	for _, permissionID := range definitions[0].AppliesTo {
		permission, exists := permissions[permissionID]
		if !exists || permission.Status != "active" {
			return fmt.Errorf("segregation-of-duties references unavailable permission %q", permissionID)
		}
	}
	return nil
}

func validateServiceIdentities(policy serviceIdentityPolicy) error {
	const rule = "A service identity is not a human role and must receive an explicit minimum per-operation, per-project grant in a later task."
	if len(policy.DefaultGrants) != 0 || policy.RequiredScope != "project" || policy.Rule != rule {
		return fmt.Errorf("service identity policy is incomplete")
	}
	return nil
}

var expectedDevelopmentProfiles = map[string][]string{
	"local-admin":     {"delivery.dashboard.read", "delivery.work-items.read", "delivery.work-items.create", "delivery.work-items.update", "delivery.work-items.comment.create", "delivery.work-items.context.update", "delivery.work-items.gate.advance", "delivery.work-items.close", "delivery.projects.create", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read", "delivery.releases.create", "delivery.sprints.create", "delivery.milestones.create", "delivery.items.read", "delivery.items.write"},
	"viewer":          {"delivery.dashboard.read", "delivery.work-items.read", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read", "delivery.items.read"},
	"contributor":     {"delivery.dashboard.read", "delivery.work-items.read", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read", "delivery.work-items.create", "delivery.work-items.update", "delivery.work-items.comment.create", "delivery.work-items.context.update", "delivery.items.read", "delivery.items.write"},
	"release-manager": {"delivery.dashboard.read", "delivery.work-items.read", "delivery.projects.read", "delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read", "delivery.work-items.create", "delivery.work-items.update", "delivery.work-items.comment.create", "delivery.work-items.context.update", "delivery.work-items.gate.advance", "delivery.work-items.close", "delivery.items.read", "delivery.items.write"},
}

func validateDevelopmentCompatibility(policy developmentCompatibility) error {
	const status = "development-only-not-a-production-role-binding"
	const rule = "Local profiles are development-only compatibility data, not production RoleBinding records or grants."
	if policy.Status != status || policy.Rule != rule || len(policy.LocalRoleProfiles) != len(expectedDevelopmentProfiles) {
		return fmt.Errorf("development compatibility policy is incomplete")
	}
	wantAliases := []legacyPermissionAlias{{LegacyPermission: "delivery.items.read", Replacement: "delivery.work-items.read", Reason: "existing ungenerated read extensions"}, {LegacyPermission: "delivery.items.write", Replacement: "delivery.work-items.update", Reason: "existing ungenerated saved-view extension"}}
	if !slices.Equal(policy.LegacyExtensionPermissionAliases, wantAliases) {
		return fmt.Errorf("development compatibility aliases are incomplete")
	}
	seen := make(map[string]struct{}, len(policy.LocalRoleProfiles))
	for _, profile := range policy.LocalRoleProfiles {
		expected, exists := expectedDevelopmentProfiles[profile.LocalRole]
		if _, duplicate := seen[profile.LocalRole]; duplicate || !exists || !slices.Equal(profile.Permissions, expected) {
			return fmt.Errorf("development local profile %q is invalid", profile.LocalRole)
		}
		seen[profile.LocalRole] = struct{}{}
	}
	return nil
}

func validateID(kind, value string) error {
	if value == "" || strings.Contains(value, "*") {
		return fmt.Errorf("%s ID %q is invalid", kind, value)
	}
	return nil
}

func validateScopes(owner string, values []string, scopes map[string]scopeDefinition) error {
	if len(values) == 0 {
		return fmt.Errorf("%s has no scopes", owner)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := scopes[value]; !exists || strings.Contains(value, "*") {
			return fmt.Errorf("%s references invalid scope %q", owner, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s duplicates scope %q", owner, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}
