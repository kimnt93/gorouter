package entities

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"

	MembershipMember = "member"
	MembershipAdmin  = "admin"

	PrincipalMaster       = "master"
	PrincipalUser         = "user"
	PrincipalOrganization = "organization"

	ActorMaster       = "master"
	ActorUser         = "user"
	ActorOrganization = "organization"
	ActorLegacy       = "legacy"

	OwnerUser         = "user"
	OwnerOrganization = "organization"
)

var (
	ErrInvalidUsername  = errors.New("username must be a valid email address")
	ErrInvalidName      = errors.New("name is required")
	ErrInvalidStatus    = errors.New("status must be active or disabled")
	ErrInvalidRole      = errors.New("role is invalid")
	ErrInvalidOwnership = errors.New("invalid API key owner or organization context")
)

type User struct {
	ID                 string    `json:"id"`
	Username           string    `json:"username"`
	NormalizedUsername string    `json:"normalized_username"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Organization struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	NormalizedName string    `json:"normalized_name"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Membership struct {
	OrganizationID     string    `json:"organization_id"`
	UserID             string    `json:"user_id"`
	Role               string    `json:"role"`
	CreatedAt          time.Time `json:"created_at"`
	CreatedByActorType string    `json:"created_by_actor_type"`
	CreatedByActorID   string    `json:"created_by_actor_id"`
}

// Principal is the currently authenticated identity. KeyID is empty for the
// master principal. OrganizationID is context, not ownership, for user keys.
type Principal struct {
	Type             string   `json:"principal_type"`
	KeyID            string   `json:"key_id,omitempty"`
	UserID           string   `json:"user_id,omitempty"`
	Username         string   `json:"username,omitempty"`
	OrganizationID   string   `json:"organization_id,omitempty"`
	OrganizationName string   `json:"organization_name,omitempty"`
	MembershipRole   string   `json:"membership_role,omitempty"`
	Scopes           []string `json:"scopes"`
}

type UsageActor struct {
	Type           string `json:"actor_type"`
	UserID         string `json:"user_id"`
	Username       string `json:"username"`
	OrganizationID string `json:"organization_id"`
}

type AuditEvent struct {
	ID             string            `json:"id"`
	TS             time.Time         `json:"ts"`
	ActorType      string            `json:"actor_type"`
	ActorID        string            `json:"actor_id"`
	ActorLabel     string            `json:"actor_label"`
	OrganizationID string            `json:"organization_id"`
	Action         string            `json:"action"`
	TargetType     string            `json:"target_type"`
	TargetID       string            `json:"target_id"`
	SafeMetadata   map[string]string `json:"safe_metadata"`
}

func NormalizeUsername(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized || strings.ContainsAny(normalized, "\r\n") {
		return "", ErrInvalidUsername
	}
	return normalized, nil
}

func NormalizeOrganizationName(value string) (string, string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", "", ErrInvalidName
	}
	return name, strings.ToLower(name), nil
}

func ValidStatus(value string) bool { return value == StatusActive || value == StatusDisabled }

func ValidMembershipRole(value string) bool {
	return value == MembershipMember || value == MembershipAdmin
}

// ValidateOwnerShape validates structural ownership. Membership and current
// owner state are intentionally checked by the identity use case.
func (k ApiKey) ValidateOwnerShape() error {
	switch k.OwnerType {
	case OwnerUser:
		if k.OwnerUserID == "" || k.OwnerOrganizationID != "" {
			return ErrInvalidOwnership
		}
	case OwnerOrganization:
		if k.OwnerOrganizationID == "" || k.OwnerUserID != "" || k.ContextOrganizationID != k.OwnerOrganizationID {
			return ErrInvalidOwnership
		}
	default:
		return ErrInvalidOwnership
	}
	return nil
}

func (p Principal) HasScope(scope string) bool {
	if p.Type == PrincipalMaster {
		return true
	}
	for _, candidate := range p.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}
