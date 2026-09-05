package localcredential

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	AlgorithmArgon2id = "argon2id"

	DefaultPolicyVersion int64  = 1
	DefaultMemoryKiB     uint32 = 19 * 1024
	DefaultIterations    uint32 = 2
	DefaultParallelism   uint8  = 1
	DefaultSaltLength    uint32 = 16
	DefaultHashLength    uint32 = 32

	maxPasswordBytes = 4096
)

var (
	ErrInvalidPassword       = errors.New("local credential password is invalid")
	ErrUnsupportedCredential = errors.New("local credential hash policy is unsupported")
	ErrCredentialCorrupt     = errors.New("local credential record is invalid")
)

// Policy is an immutable password-hash profile. PolicyVersion is deliberately
// independent of Argon's protocol version: future work-factor changes get a
// new policy version while the stored Argon version remains explicit.
type Policy struct {
	PolicyVersion int64
	Algorithm     string
	ArgonVersion  int
	MemoryKiB     uint32
	Iterations    uint32
	Parallelism   uint8
	SaltLength    uint32
	HashLength    uint32
}

func DefaultPolicy() Policy {
	return Policy{
		PolicyVersion: DefaultPolicyVersion,
		Algorithm:     AlgorithmArgon2id,
		ArgonVersion:  argon2.Version,
		MemoryKiB:     DefaultMemoryKiB,
		Iterations:    DefaultIterations,
		Parallelism:   DefaultParallelism,
		SaltLength:    DefaultSaltLength,
		HashLength:    DefaultHashLength,
	}
}

// PolicySet keeps every policy that the process can still verify and identifies
// exactly one current policy for new hashes. Retaining old profiles is what
// makes login-time rehash upgrades possible without guessing parameter strength.
type PolicySet struct {
	current  int64
	policies map[int64]Policy
}

func DefaultPolicySet() (PolicySet, error) {
	return NewPolicySet(DefaultPolicyVersion, DefaultPolicy())
}

func NewPolicySet(current int64, policies ...Policy) (PolicySet, error) {
	if current < 1 || len(policies) == 0 {
		return PolicySet{}, errors.New("local credential policy set is invalid")
	}
	set := PolicySet{current: current, policies: make(map[int64]Policy, len(policies))}
	for _, policy := range policies {
		if err := validatePolicy(policy); err != nil {
			return PolicySet{}, err
		}
		if _, duplicate := set.policies[policy.PolicyVersion]; duplicate {
			return PolicySet{}, errors.New("local credential policy version is duplicated")
		}
		set.policies[policy.PolicyVersion] = policy
	}
	if _, ok := set.policies[current]; !ok {
		return PolicySet{}, errors.New("local credential current policy is missing")
	}
	return set, nil
}

func (set PolicySet) Current() (Policy, error) {
	policy, ok := set.policies[set.current]
	if !ok {
		return Policy{}, errors.New("local credential current policy is unavailable")
	}
	return policy, nil
}

func (set PolicySet) policy(version int64) (Policy, error) {
	policy, ok := set.policies[version]
	if !ok {
		return Policy{}, ErrUnsupportedCredential
	}
	return policy, nil
}

func (set PolicySet) needsRehash(version int64) bool { return version < set.current }

func validatePolicy(policy Policy) error {
	if policy.PolicyVersion < 1 || policy.Algorithm != AlgorithmArgon2id || policy.ArgonVersion != argon2.Version {
		return errors.New("local credential policy algorithm is unsupported")
	}
	if policy.Parallelism != 1 || policy.SaltLength < 16 || policy.SaltLength > 64 || policy.HashLength < 32 || policy.HashLength > 64 {
		return errors.New("local credential policy parameters are invalid")
	}
	if policy.MemoryKiB > 1024*1024 || policy.Iterations > 100 || !meetsOWASPArgon2idFloor(policy.MemoryKiB, policy.Iterations) {
		return errors.New("local credential Argon2id work factor is below the supported security floor")
	}
	return nil
}

// meetsOWASPArgon2idFloor encodes the currently supported OWASP Argon2id
// trade-off profiles. The current policy uses 19 MiB / 2 iterations / p=1,
// while future policy versions may select another listed-equivalent or stronger
// memory/iteration point without changing the persistence model.
func meetsOWASPArgon2idFloor(memoryKiB, iterations uint32) bool {
	return (memoryKiB >= 47104 && iterations >= 1) ||
		(memoryKiB >= 19456 && iterations >= 2) ||
		(memoryKiB >= 12288 && iterations >= 3) ||
		(memoryKiB >= 9216 && iterations >= 4) ||
		(memoryKiB >= 7168 && iterations >= 5)
}

type hashMaterial struct {
	policy Policy
	salt   []byte
	hash   []byte
}

func (set PolicySet) hashPassword(password []byte, random io.Reader) (hashMaterial, error) {
	if len(password) == 0 || len(password) > maxPasswordBytes {
		return hashMaterial{}, ErrInvalidPassword
	}
	if random == nil {
		random = rand.Reader
	}
	policy, err := set.Current()
	if err != nil {
		return hashMaterial{}, err
	}
	salt := make([]byte, policy.SaltLength)
	if _, err := io.ReadFull(random, salt); err != nil {
		return hashMaterial{}, errors.New("generate local credential salt")
	}
	hash := argon2.IDKey(password, salt, policy.Iterations, policy.MemoryKiB, policy.Parallelism, policy.HashLength)
	return hashMaterial{policy: policy, salt: salt, hash: hash}, nil
}

func (set PolicySet) verifyPassword(password []byte, material hashMaterial) (bool, error) {
	if len(password) == 0 || len(password) > maxPasswordBytes {
		return false, ErrInvalidPassword
	}
	policy, err := set.policy(material.policy.PolicyVersion)
	if err != nil {
		return false, err
	}
	if material.policy != policy || len(material.salt) != int(policy.SaltLength) || len(material.hash) != int(policy.HashLength) {
		return false, ErrCredentialCorrupt
	}
	candidate := argon2.IDKey(password, material.salt, policy.Iterations, policy.MemoryKiB, policy.Parallelism, policy.HashLength)
	defer zeroBytes(candidate)
	return subtle.ConstantTimeCompare(candidate, material.hash) == 1, nil
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func describePolicy(policy Policy) string {
	return fmt.Sprintf("%s/v%d/policy-%d", policy.Algorithm, policy.ArgonVersion, policy.PolicyVersion)
}
