package configrevision

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	maxPayloadBytes = 16 << 10
	maxPayloadDepth = 16
	maxPayloadNodes = 128
)

var secretValuePattern = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9._~+/=-]+|basic\s+[a-z0-9+/=]+|\beyJ[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+\b|\bsvc\.[a-z0-9._-]+)`)
var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

var ErrNotFound = errors.New("config revision not found")
var ErrRevisionConflict = errors.New("config revision conflict")

type Kind string

const (
	KindIdentityProvider Kind = "identity_provider"
	KindMembership       Kind = "membership"
	KindRoleBinding      Kind = "role_binding"
	KindDomainDictionary Kind = "domain_dictionary"
)

type CreatedByType string

const (
	CreatedByHuman   CreatedByType = "human"
	CreatedByService CreatedByType = "service"
	CreatedBySystem  CreatedByType = "system"
)

type ConfigRevision struct {
	ID             string
	OrganizationID string
	Kind           Kind
	ConfigKey      string
	Revision       int64
	ParentRevision int64
	Payload        string
	PayloadHash    [sha256.Size]byte
	CreatedByType  CreatedByType
	CreatedByID    string
	CreatedAt      time.Time
}

type AppendInput struct {
	ID                     string
	OrganizationID         string
	Kind                   Kind
	ConfigKey              string
	ExpectedParentRevision int64
	Payload                string
	CreatedByType          CreatedByType
	CreatedByID            string
	// CreatedAt is only present so caller-supplied timestamps fail closed; the
	// store assigns the durable timestamp from its injected UTC clock.
	CreatedAt time.Time
}

// ChangeInput is the client-facing request for an immutable revision append.
// Organization, actor, revision, hash, and timestamps are derived only after
// the operation guard has authenticated and authorized the trusted principal.
type ChangeInput struct {
	Kind                   Kind
	ConfigKey              string
	ExpectedParentRevision int64
	Payload                string
}

// CompareInput names two existing revisions in the same trusted organization,
// kind, and key. It carries no payload and cannot select an organization.
type CompareInput struct {
	Kind          Kind
	ConfigKey     string
	LeftRevision  int64
	RightRevision int64
}

// RollbackInput appends the canonical payload of SourceRevision with the
// latest revision supplied by the caller as the compare-and-swap parent.
type RollbackInput struct {
	Kind                   Kind
	ConfigKey              string
	ExpectedParentRevision int64
	SourceRevision         int64
}

func normalizePayload(payload string) (string, [sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if len(payload) == 0 || len(payload) > maxPayloadBytes {
		return "", empty, errors.New("config revision payload size is invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder, 0, new(int))
	if err != nil {
		return "", empty, errors.New("config revision payload is invalid")
	}
	if _, ok := value.(map[string]any); !ok {
		return "", empty, errors.New("config revision payload must be one JSON object")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return "", empty, errors.New("config revision payload has trailing data")
	}
	if containsCredentialMaterial(value, "") {
		return "", empty, errors.New("config revision payload contains prohibited credential material")
	}
	var canonical bytes.Buffer
	if err := writeCanonicalJSON(&canonical, value); err != nil {
		return "", empty, errors.New("config revision payload is invalid")
	}
	result := canonical.String()
	if len(result) > maxPayloadBytes {
		return "", empty, errors.New("config revision canonical payload is too large")
	}
	return result, sha256.Sum256([]byte(result)), nil
}

func decodeJSONValue(decoder *json.Decoder, depth int, nodes *int) (any, error) {
	if depth > maxPayloadDepth {
		return nil, errors.New("JSON nesting limit exceeded")
	}
	*nodes++
	if *nodes > maxPayloadNodes {
		return nil, errors.New("JSON node limit exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token := token.(type) {
	case json.Delim:
		switch token {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("object key is invalid")
				}
				if _, exists := object[key]; exists {
					return nil, errors.New("duplicate object key")
				}
				child, err := decodeJSONValue(decoder, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				object[key] = child
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
				return nil, errors.New("object is unterminated")
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for decoder.More() {
				child, err := decodeJSONValue(decoder, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				array = append(array, child)
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
				return nil, errors.New("array is unterminated")
			}
			return array, nil
		default:
			return nil, errors.New("invalid JSON delimiter")
		}
	case json.Number:
		if _, err := canonicalNumber(string(token)); err != nil {
			return nil, err
		}
		return token, nil
	case string, bool, nil:
		return token, nil
	default:
		return nil, errors.New("invalid JSON value")
	}
}

func containsCredentialMaterial(value any, key string) bool {
	if isCredentialKey(key) {
		return true
	}
	switch value := value.(type) {
	case map[string]any:
		for childKey, child := range value {
			if containsCredentialMaterial(child, childKey) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if containsCredentialMaterial(child, "") {
				return true
			}
		}
	case string:
		return secretValuePattern.MatchString(value)
	}
	return false
}

func isCredentialKey(key string) bool {
	var normalized strings.Builder
	for _, character := range strings.ToLower(key) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			normalized.WriteRune(character)
		}
	}
	for _, sensitive := range []string{"password", "passphrase", "secret", "token", "credential", "apikey", "clientsecret", "cookie", "authorization", "session", "csrf", "assertion", "signature"} {
		if strings.Contains(normalized.String(), sensitive) {
			return true
		}
	}
	return false
}

func writeCanonicalJSON(buffer *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case map[string]any:
		keys := slices.Sorted(maps.Keys(value))
		buffer.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				buffer.WriteByte(',')
			}
			encodedKey, _ := json.Marshal(key)
			buffer.Write(encodedKey)
			buffer.WriteByte(':')
			if err := writeCanonicalJSON(buffer, value[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	case []any:
		buffer.WriteByte('[')
		for index, child := range value {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := writeCanonicalJSON(buffer, child); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case json.Number:
		number, err := canonicalNumber(string(value))
		if err != nil {
			return err
		}
		buffer.WriteString(number)
	case string:
		encoded, _ := json.Marshal(value)
		buffer.Write(encoded)
	case bool:
		buffer.WriteString(strconv.FormatBool(value))
	case nil:
		buffer.WriteString("null")
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

func canonicalNumber(value string) (string, error) {
	if !regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`).MatchString(value) {
		return "", errors.New("invalid JSON number")
	}
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	exponent := 0
	if marker := strings.IndexAny(value, "eE"); marker >= 0 {
		parsed, err := strconv.Atoi(value[marker+1:])
		if err != nil || parsed < -maxPayloadBytes || parsed > maxPayloadBytes {
			return "", errors.New("JSON exponent is invalid")
		}
		exponent, value = parsed, value[:marker]
	}
	fraction := 0
	if marker := strings.IndexByte(value, '.'); marker >= 0 {
		fraction = len(value) - marker - 1
		value = value[:marker] + value[marker+1:]
	}
	digits := strings.TrimLeft(value, "0")
	if digits == "" {
		return "0", nil
	}
	scale := fraction - exponent
	for scale > 0 && strings.HasSuffix(digits, "0") {
		digits = strings.TrimSuffix(digits, "0")
		scale--
	}
	if scale > 0 {
		if scale >= len(digits) {
			value = "0." + strings.Repeat("0", scale-len(digits)) + digits
		} else {
			value = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
		}
	} else if scale < 0 {
		value = digits + strings.Repeat("0", -scale)
	} else {
		value = digits
	}
	if negative {
		value = "-" + value
	}
	return value, nil
}

func validIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 || !identifierPattern.MatchString(value) || secretValuePattern.MatchString(value) {
		return false
	}
	return true
}

func validKind(kind Kind) bool {
	return kind == KindIdentityProvider || kind == KindMembership || kind == KindRoleBinding || kind == KindDomainDictionary
}

func validCreatedByType(actor CreatedByType) bool {
	return actor == CreatedByHuman || actor == CreatedByService || actor == CreatedBySystem
}
