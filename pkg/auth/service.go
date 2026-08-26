package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"

	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

var (
	ErrInvalidKey = errors.New("invalid key")
	ErrDisabled   = errors.New("key disabled")
	ErrExpired    = errors.New("session expired")
	ErrBadToken   = errors.New("bad token")
)

const SessionTTL = 12 * time.Hour

type ApiKeyLookup interface {
	GetBySecret(ctx context.Context, secret string) (*entities.ApiKey, error)
}

type ApiKeySessionLookup interface {
	GetByID(ctx context.Context, id string) (*entities.ApiKey, error)
}

type IdentityLookup interface {
	UserByID(ctx context.Context, id string) (*entities.User, error)
	OrganizationByID(ctx context.Context, id string) (*entities.Organization, error)
	Membership(ctx context.Context, organizationID, userID string) (*entities.Membership, error)
}

type Service struct {
	masterHash [sha256.Size]byte
	hasMaster  bool
	secret     []byte
	keys       ApiKeyLookup
	identity   IdentityLookup
}

func NewServiceWithIdentity(masterKey, sessionSecret string, keys ApiKeyLookup, identity IdentityLookup) *Service {
	service := NewService(masterKey, sessionSecret, keys)
	service.identity = identity
	return service
}

func NewService(masterKey, sessionSecret string, keys ApiKeyLookup) *Service {
	h := sha256.Sum256([]byte("nr-session::" + sessionSecret))
	return &Service{masterHash: sha256.Sum256([]byte(masterKey)), hasMaster: masterKey != "", secret: h[:], keys: keys}
}

// Login accepts the setup master key or any enabled API key and returns a session.
// The master key holds every scope; API keys are limited to their configured scopes.
func (s *Service) Login(ctx context.Context, secret string) (*entities.Session, error) {
	presentedHash := sha256.Sum256([]byte(secret))
	if s.hasMaster && subtle.ConstantTimeCompare(presentedHash[:], s.masterHash[:]) == 1 {
		return &entities.Session{
			Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster, Username: "master",
			Scopes: entities.AllScopes, Expires: time.Now().Add(SessionTTL).Unix(),
		}, nil
	}
	if s.keys != nil && strings.TrimSpace(secret) != "" {
		key, err := s.keys.GetBySecret(ctx, secret)
		if err == nil {
			if !key.Enabled {
				return nil, ErrDisabled
			}
			return s.sessionForKey(ctx, key, time.Now().Add(SessionTTL).Unix())
		}
	}
	return nil, ErrInvalidKey
}

// Revalidate refreshes authorization data for a cookie session without
// extending its expiration. This makes key disablement and scope changes take
// effect before the signed cookie expires.
func (s *Service) Revalidate(ctx context.Context, sess *entities.Session) (*entities.Session, error) {
	if sess == nil || time.Now().Unix() > sess.Expires {
		return nil, ErrExpired
	}
	if sess.IsMaster() {
		return sess, nil
	}
	if sess.Role != entities.RoleAPIKey || sess.KeyID == "" {
		return nil, ErrBadToken
	}
	lookup, ok := s.keys.(ApiKeySessionLookup)
	if !ok {
		return nil, ErrBadToken
	}
	key, err := lookup.GetByID(ctx, sess.KeyID)
	if err != nil {
		return nil, ErrInvalidKey
	}
	if !key.Enabled {
		return nil, ErrDisabled
	}
	if sess.TenantID != "" && sess.TenantID != key.TenantID {
		return nil, ErrBadToken
	}
	resolved, err := s.sessionForKey(ctx, key, sess.Expires)
	if err != nil {
		return nil, err
	}
	if sess.PrincipalType != "" && (sess.PrincipalType != resolved.PrincipalType || sess.UserID != resolved.UserID || sess.OrganizationID != resolved.OrganizationID) {
		return nil, ErrBadToken
	}
	return resolved, nil
}

func (s *Service) sessionForKey(ctx context.Context, key *entities.ApiKey, expires int64) (*entities.Session, error) {
	if !key.Enabled {
		return nil, ErrDisabled
	}
	ownerType := key.OwnerType
	if ownerType == "" {
		ownerType = entities.OwnerOrganization
		key.OwnerOrganizationID = key.TenantID
		key.ContextOrganizationID = key.TenantID
	}
	sess := &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, Scopes: append([]string(nil), key.Scopes...), Expires: expires, PrincipalType: ownerType, OrganizationID: key.ContextOrganizationID}
	if s.identity == nil {
		if key.OwnerType != "" {
			return nil, ErrInvalidKey
		}
		return sess, nil
	}
	switch ownerType {
	case entities.OwnerUser:
		user, err := s.identity.UserByID(ctx, key.OwnerUserID)
		if err != nil || user.Status != entities.StatusActive {
			return nil, ErrDisabled
		}
		sess.UserID, sess.Username = user.ID, user.Username
		if key.ContextOrganizationID != "" {
			organization, orgErr := s.identity.OrganizationByID(ctx, key.ContextOrganizationID)
			if orgErr != nil || organization.Status != entities.StatusActive {
				return nil, ErrDisabled
			}
			membership, membershipErr := s.identity.Membership(ctx, organization.ID, user.ID)
			if membershipErr != nil {
				return nil, ErrDisabled
			}
			sess.MembershipRole = membership.Role
		}
	case entities.OwnerOrganization:
		organization, err := s.identity.OrganizationByID(ctx, key.OwnerOrganizationID)
		if err != nil || organization.Status != entities.StatusActive || key.ContextOrganizationID != organization.ID {
			return nil, ErrDisabled
		}
		sess.OrganizationID, sess.Username = organization.ID, "org:"+organization.Name
	case entities.PrincipalMaster:
		return nil, ErrInvalidKey
	default:
		return nil, ErrInvalidKey
	}
	return sess, nil
}

func (s *Service) VerifyAndRevalidate(ctx context.Context, token string) (*entities.Session, error) {
	sess, err := s.VerifyToken(token)
	if err != nil {
		return nil, err
	}
	return s.Revalidate(ctx, sess)
}

// AuthorizeBearer validates a raw secret presented as a Bearer token for API calls.
// Master key passes with all scopes; API keys must be enabled.
func (s *Service) AuthorizeBearer(ctx context.Context, secret string) (*entities.Session, error) {
	sess, err := s.Login(ctx, secret)
	if err == nil {
		return sess, nil
	}
	return nil, err
}

func (s *Service) IssueToken(sess *entities.Session) (string, error) {
	payload, err := json.Marshal(sess)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + s.sign(body), nil
}

func (s *Service) VerifyToken(token string) (*entities.Session, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return nil, ErrBadToken
	}
	expected := s.sign(body)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return nil, ErrBadToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, ErrBadToken
	}
	var sess entities.Session
	if err := json.Unmarshal(payload, &sess); err != nil {
		return nil, ErrBadToken
	}
	if time.Now().Unix() > sess.Expires {
		return nil, ErrExpired
	}
	return &sess, nil
}

func (s *Service) sign(body string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
