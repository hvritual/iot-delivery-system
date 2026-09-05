// Package identitycore owns persistent internal identity records for the
// delivery application. It is deliberately separate from Yunka core identity:
// this package does not authenticate callers or represent execution principals.
package identitycore

import "time"

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

type Organization struct {
	ID        string
	Slug      string
	Name      string
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

type User struct {
	ID             string
	OrganizationID string
	DisplayName    string
	Email          string
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ExternalIdentity struct {
	ID                  string
	OrganizationID      string
	UserID              string
	Issuer              string
	Subject             string
	EmailSnapshot       string
	DisplayNameSnapshot string
	LastSeenAt          *time.Time
	Status              Status
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ServiceAccount struct {
	ID             string
	OrganizationID string
	Name           string
	Description    string
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ServiceAccountCredential is the durable, non-secret record for a service
// credential. CredentialHash is a one-way SHA-256 digest; the issued secret is
// never represented by this persistent model.
type ServiceAccountCredential struct {
	ID               string
	ServiceAccountID string
	CredentialHash   []byte
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
}

type ScopeType string

const (
	ScopeTypeOrganization ScopeType = "organization"
	ScopeTypeProject      ScopeType = "project"
	ScopeTypeObject       ScopeType = "object"
)

type PermissionStatus string

const (
	PermissionStatusActive   PermissionStatus = "active"
	PermissionStatusReserved PermissionStatus = "reserved"
)

// Team belongs to an organization and is scoped either to that organization
// or to a project. Project ownership is introduced in S0-03-04.
type Team struct {
	ID             string
	OrganizationID string
	Name           string
	ScopeType      ScopeType
	ScopeID        string
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Membership struct {
	TeamID         string
	OrganizationID string
	UserID         string
	CreatedAt      time.Time
}

// Role is a built-in dictionary definition that may be bound at BindingScope.
type Role struct {
	ID           string
	BindingScope ScopeType
}

// Permission is a built-in dictionary definition. AllowedScopes is carried by
// the relationship table instead of being an independently editable grant.
type Permission struct {
	ID            string
	Resource      string
	Action        string
	AllowedScopes []ScopeType
	Status        PermissionStatus
}

type RolePermissionGrant struct {
	RoleID        string
	PermissionID  string
	AllowedScopes []ScopeType
}

// RoleBinding grants a built-in role to exactly one human user or team. A
// service account is intentionally not a valid subject for this model. Revision
// is the durable CAS token for mutable binding lifecycle state.
type RoleBinding struct {
	ID             string
	OrganizationID string
	RoleID         string
	ScopeType      ScopeType
	ScopeID        string
	UserID         *string
	TeamID         *string
	Status         Status
	Revision       int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
