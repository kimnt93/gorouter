package clickhouse

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type MutationLocker interface {
	WithLock(ctx context.Context, key string, fn func() error) error
}

type Store struct {
	Conn   ch.Conn
	locker MutationLocker
}

func New(conn ch.Conn) *Store { return &Store{Conn: conn, locker: newProcessLocker()} }
func NewWithLocker(conn ch.Conn, locker MutationLocker) *Store {
	return &Store{Conn: conn, locker: locker}
}

func (s *Store) mutate(ctx context.Context, key string, fn func() error) error {
	if s.locker == nil {
		return errors.New("clickhouse configuration mutation lock is unavailable")
	}
	return s.locker.WithLock(ctx, key, fn)
}

type processLocker struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newProcessLocker() *processLocker { return &processLocker{locks: make(map[string]*sync.Mutex)} }
func (l *processLocker) WithLock(_ context.Context, key string, fn func() error) error {
	l.mu.Lock()
	lock := l.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		l.locks[key] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	return fn()
}

func id(prefix string) string    { return entities.NewID(prefix) }
func HashSecret(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func GenerateSecret() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "nr-" + hex.EncodeToString(b)
}

func (s *Store) put(ctx context.Context, entity, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.Conn.Exec(ctx, `INSERT INTO config_records (entity,key,payload,version,deleted) VALUES (?,?,?,?,0)`, entity, key, string(b), time.Now().UTC())
}
func (s *Store) del(ctx context.Context, entity, key string) error {
	if _, err := s.raw(ctx, entity, key); err != nil {
		return err
	}
	return s.Conn.Exec(ctx, `INSERT INTO config_records (entity,key,payload,version,deleted) VALUES (?,?,?,?,1)`, entity, key, "{}", time.Now().UTC())
}
func (s *Store) raw(ctx context.Context, entity, key string) (string, error) {
	var payload string
	var deleted uint8
	err := s.Conn.QueryRow(ctx, `SELECT payload,deleted FROM config_records WHERE entity=? AND key=? ORDER BY version DESC LIMIT 1`, entity, key).Scan(&payload, &deleted)
	if err != nil || deleted != 0 {
		if err == nil || errors.Is(err, sql.ErrNoRows) {
			return "", entities.ErrNotFound
		}
		return "", err
	}
	return payload, nil
}
func get[T any](ctx context.Context, s *Store, entity, key string) (*T, error) {
	p, err := s.raw(ctx, entity, key)
	if err != nil {
		return nil, err
	}
	var v T
	if err = json.Unmarshal([]byte(p), &v); err != nil {
		return nil, err
	}
	return &v, nil
}
func list[T any](ctx context.Context, s *Store, entity string) ([]T, error) {
	rows, err := s.Conn.Query(ctx, `SELECT argMax(payload,version) FROM config_records WHERE entity=? GROUP BY key HAVING argMax(deleted,version)=0`, entity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal([]byte(p), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type TenantRepo struct{ s *Store }

func NewTenantRepo(s *Store) *TenantRepo { return &TenantRepo{s} }
func (r *TenantRepo) List(ctx context.Context) ([]entities.Tenant, error) {
	return list[entities.Tenant](ctx, r.s, "tenant")
}
func (r *TenantRepo) Create(ctx context.Context, name string) (*entities.Tenant, error) {
	t := &entities.Tenant{ID: id("tenant"), Name: name, CreatedAt: time.Now().UTC()}
	return t, r.s.put(ctx, "tenant", t.ID, t)
}
func (r *TenantRepo) EnsureDefault(ctx context.Context) error {
	if _, err := get[entities.Tenant](ctx, r.s, "tenant", "tenant_default"); err == nil {
		return nil
	}
	return r.s.put(ctx, "tenant", "tenant_default", entities.Tenant{ID: "tenant_default", Name: "default", CreatedAt: time.Now().UTC()})
}

type storedCredential struct {
	entities.Credential
	APIKeyEnc []byte `json:"api_key_enc"`
	OAuthEnc  []byte `json:"oauth_enc"`
}
type oauthBlob struct {
	Access   string                 `json:"access"`
	Refresh  string                 `json:"refresh"`
	IDToken  string                 `json:"id_token,omitempty"`
	Account  string                 `json:"account,omitempty"`
	Metadata entities.OAuthMetadata `json:"metadata,omitempty"`
}
type CredentialRepo struct{ s *Store }

func NewCredentialRepo(s *Store) *CredentialRepo { return &CredentialRepo{s} }
func preview(v string) string {
	if len(v) <= 8 {
		return "…"
	}
	return v[:6] + "…" + v[len(v)-4:]
}
func sealInput(in entities.CredentialInput, box entities.SecretBox) ([]byte, []byte, string, error) {
	var key, oauth []byte
	var p string
	var err error
	if in.APIKey != "" {
		key, err = box.Seal([]byte(in.APIKey))
		p = preview(in.APIKey)
	}
	if err == nil && (in.OAuthAccess != "" || in.OAuthRefresh != "") {
		b, _ := json.Marshal(oauthBlob{in.OAuthAccess, in.OAuthRefresh, in.OAuthIDToken, in.OAuthAccount, in.OAuthMeta})
		oauth, err = box.Seal(b)
		p = preview(in.OAuthAccess)
	}
	return key, oauth, p, err
}
func (r *CredentialRepo) Create(ctx context.Context, in entities.CredentialInput, box entities.SecretBox) (*entities.Credential, error) {
	key, oauth, p, err := sealInput(in, box)
	if err != nil {
		return nil, err
	}
	c := entities.Credential{ID: id("cred"), Name: in.Name, Provider: in.Provider, Kind: in.Kind, BaseURL: in.BaseURL, Status: "active", KeyPreview: p, OwnerTenantID: in.OwnerTenant, CreatedAt: time.Now().UTC()}
	c.SetSecrets(key, oauth)
	return &c, r.s.put(ctx, "credential", c.ID, storedCredential{c, key, oauth})
}
func (r *CredentialRepo) stored(ctx context.Context, id string) (*storedCredential, error) {
	return get[storedCredential](ctx, r.s, "credential", id)
}
func (r *CredentialRepo) Update(ctx context.Context, box entities.SecretBox, idv string, in entities.CredentialUpdate) (*entities.Credential, error) {
	v, err := r.stored(ctx, idv)
	if err != nil {
		return nil, err
	}
	v.Name = in.Name
	v.BaseURL = in.BaseURL
	v.Status = in.Status
	v.OwnerTenantID = in.OwnerTenant
	if v.Kind == entities.KindAPIKey && in.APIKey != "" {
		v.APIKeyEnc, err = box.Seal([]byte(in.APIKey))
		v.KeyPreview = preview(in.APIKey)
	}
	if v.Kind == entities.KindOAuth && (in.OAuthAccess != "" || in.OAuthRefresh != "") {
		if in.OAuthRefresh == "" {
			return nil, errors.New("oauth_refresh is required when rotating OAuth tokens")
		}
		b, _ := json.Marshal(oauthBlob{Access: in.OAuthAccess, Refresh: in.OAuthRefresh})
		v.OAuthEnc, err = box.Seal(b)
		v.KeyPreview = preview(in.OAuthAccess)
	}
	if err != nil {
		return nil, err
	}
	v.SetSecrets(v.APIKeyEnc, v.OAuthEnc)
	return &v.Credential, r.s.put(ctx, "credential", idv, v)
}
func (r *CredentialRepo) List(ctx context.Context) ([]entities.Credential, error) {
	v, err := list[storedCredential](ctx, r.s, "credential")
	out := make([]entities.Credential, 0, len(v))
	for _, x := range v {
		x.SetSecrets(x.APIKeyEnc, x.OAuthEnc)
		out = append(out, x.Credential)
	}
	return out, err
}
func (r *CredentialRepo) Delete(ctx context.Context, id string) error {
	if _, err := r.stored(ctx, id); err != nil {
		return err
	}
	models, err := list[entities.ModelDef](ctx, r.s, "model")
	if err != nil {
		return err
	}
	for _, m := range models {
		routes := m.Routes[:0]
		changed := false
		for _, route := range m.Routes {
			if route.CredentialID == id {
				changed = true
				continue
			}
			routes = append(routes, route)
		}
		if changed {
			m.Routes = routes
			if err := r.s.put(ctx, "model", m.Name, m); err != nil {
				return err
			}
		}
	}
	return r.s.del(ctx, "credential", id)
}
func (r *CredentialRepo) Runtime(ctx context.Context, box entities.SecretBox, id string) (*entities.CredentialRuntime, error) {
	v, err := r.stored(ctx, id)
	if err != nil {
		return nil, err
	}
	rt := &entities.CredentialRuntime{ID: v.ID, Provider: v.Provider, Kind: v.Kind, BaseURL: v.BaseURL}
	if v.Kind == entities.KindAPIKey {
		b, e := box.Open(v.APIKeyEnc)
		if e != nil {
			return nil, e
		}
		rt.APIKey = string(b)
	} else {
		b, e := box.Open(v.OAuthEnc)
		if e != nil {
			return nil, e
		}
		var o oauthBlob
		if e = json.Unmarshal(b, &o); e != nil {
			return nil, e
		}
		rt.OAuthAccess = o.Access
		rt.OAuthRefreh = o.Refresh
		rt.OAuthIDToken = o.IDToken
		rt.OAuthAccount = o.Account
		rt.OAuthMeta = o.Metadata
	}
	return rt, nil
}
func (r *CredentialRepo) UpdateOAuthTokens(ctx context.Context, box entities.SecretBox, id, access, refresh string) error {
	v, err := r.stored(ctx, id)
	if err != nil {
		return err
	}
	var o oauthBlob
	if len(v.OAuthEnc) > 0 {
		b, e := box.Open(v.OAuthEnc)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &o); e != nil {
			return e
		}
	}
	o.Access = access
	o.Refresh = refresh
	b, _ := json.Marshal(o)
	v.OAuthEnc, err = box.Seal(b)
	if err != nil {
		return err
	}
	return r.s.put(ctx, "credential", id, v)
}
func (r *CredentialRepo) RoutesForModel(ctx context.Context, model string) ([]entities.RouteCandidate, error) {
	m, err := get[entities.ModelDef](ctx, r.s, "model", model)
	if err != nil {
		return nil, err
	}
	out := []entities.RouteCandidate{}
	for _, rt := range m.Routes {
		if !rt.Enabled {
			continue
		}
		c, e := r.stored(ctx, rt.CredentialID)
		if e == nil && c.Status == "active" {
			out = append(out, entities.RouteCandidate{CredentialID: rt.CredentialID, Priority: rt.Priority, Weight: rt.Weight, OwnerTenant: c.OwnerTenantID})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority == out[j].Priority {
			return out[i].CredentialID < out[j].CredentialID
		}
		return out[i].Priority > out[j].Priority
	})
	return out, nil
}
