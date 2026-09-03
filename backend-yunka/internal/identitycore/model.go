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
