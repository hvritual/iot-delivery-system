// Package authorization exposes the versioned authorization dictionary that
// defines the built-in roles, permissions, and allowed scopes for this
// application. The JSON file remains the single authoritative source.
package authorization

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed permission-dictionary.v1.json
var permissionDictionaryJSON []byte

const schemaVersion = "1.0.0"

type Dictionary struct {
	SchemaVersion string       `json:"schemaVersion"`
	DictionaryID  string       `json:"dictionaryId"`
	Permissions   []Permission `json:"permissions"`
	Operations    []Operation  `json:"operations"`
	Roles         []Role       `json:"roles"`
}

type Operation struct {
	ID                 string   `json:"id"`
	Permission         string   `json:"permission"`
	RequiredScope      string   `json:"requiredScope"`
	RequiresOperations []string `json:"requiresOperations,omitempty"`
}

type Permission struct {
	ID            string   `json:"id"`
	Resource      string   `json:"resource"`
	Action        string   `json:"action"`
	AllowedScopes []string `json:"allowedScopes"`
	Status        string   `json:"status"`
}

type Role struct {
	ID           string      `json:"id"`
	BindingScope string      `json:"bindingScope"`
	Grants       []RoleGrant `json:"grants"`
}

type RoleGrant struct {
	Permission    string   `json:"permission"`
	AllowedScopes []string `json:"allowedScopes"`
}

func LoadPermissionDictionary() (Dictionary, error) {
	var dictionary Dictionary
	if err := json.Unmarshal(permissionDictionaryJSON, &dictionary); err != nil {
		return Dictionary{}, fmt.Errorf("decode permission dictionary: %w", err)
	}
	if dictionary.SchemaVersion != schemaVersion {
		return Dictionary{}, fmt.Errorf("permission dictionary schema version = %q, want %q", dictionary.SchemaVersion, schemaVersion)
	}
	if dictionary.DictionaryID == "" {
		return Dictionary{}, fmt.Errorf("permission dictionary ID is required")
	}
	return dictionary, nil
}
