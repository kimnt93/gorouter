// Package orgmodel owns one-to-one aliases and bulk-assignment packages,
// user assignments, and durable budget reservations. Agents are not principals.
package orgmodel

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/provider"
	"github.com/kimnt93/gorouter/pkg/quota"
)

var ErrForbidden = errors.New("organization model access denied")
var ErrInvalid = errors.New("invalid organization model definition")
var ErrBudget = errors.New("organization model budget exceeded")

type Repository interface {
	List(context.Context, string) ([]entities.OrganizationModel, error)
	Put(context.Context, entities.OrganizationModel) error
	Grants(context.Context, string, string) ([]entities.OrganizationModelGrant, error)
	PutGrant(context.Context, entities.OrganizationModelGrant) error
	Reserve(context.Context, entities.ModelBudgetReservation) error
	Settle(context.Context, string, string, float64) error
}
type Identity interface {
	OrganizationByID(context.Context, string) (*entities.Organization, error)
	ListMembershipsForUser(context.Context, string) ([]entities.Membership, error)
	Membership(context.Context, string, string) (*entities.Membership, error)
	UserByID(context.Context, string) (*entities.User, error)
}
type Credentials interface {
	List(context.Context) ([]entities.Credential, error)
}
type Models interface {
	List(context.Context) ([]entities.ModelDef, error)
}
type Service struct {
	Audit       entities.AuditRepository
	Repo        Repository
	Identity    Identity
	Credentials Credentials
	Models      Models
}

func validLimit(v *float64) bool {
	return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0)
}
func validName(v string) bool {
	if len(v) == 0 || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if !(r == '-' || r == '_' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return v != "g" && v != "auto"
}
func (s *Service) Manage(ctx context.Context, actor entities.Principal, orgID string) (*entities.Organization, error) {
	if !actor.HasScope(entities.ScopeModelsManage) {
		return nil, ErrForbidden
	}
	org, err := s.Identity.OrganizationByID(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if org.Status != entities.StatusActive {
		return nil, ErrForbidden
	}
	if actor.Type == entities.PrincipalMaster {
		return org, nil
	}
	if actor.Type != entities.PrincipalUser || actor.OrganizationID != "" && actor.OrganizationID != orgID {
		return nil, ErrForbidden
	}
	m, err := s.Identity.Membership(ctx, orgID, actor.UserID)
	if err != nil || m.Role != entities.MembershipAdmin {
		return nil, ErrForbidden
	}
	return org, nil
}
func (s *Service) Publish(ctx context.Context, actor entities.Principal, orgID, slug, kind string, targets []string, limit *float64, enabled bool) (*entities.OrganizationModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	org, err := s.Manage(ctx, actor, orgID)
	if err != nil {
		return nil, err
	}
	if limit != nil && *limit != 0 {
		return nil, ErrInvalid
	}
	if !validName(slug) || !validLimit(limit) || len(targets) == 0 || len(targets) > 32 || (kind != "alias" && kind != "group") || kind == "alias" && len(targets) != 1 {
		return nil, ErrInvalid
	}
	if provider.OrganizationSlug(org.Name) == "" {
		return nil, ErrInvalid
	}
	name := provider.OrganizationAliasID(org.Name, slug)
	if kind == "group" {
		name = provider.OrganizationGroupID(org.Name, slug)
	}
	records, err := s.Repo.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	// Only aliases may refer to concrete models. Groups contain aliases or direct
	// publisher-owned sources; nested groups are rejected to keep routing bounded.
	known := map[string]entities.OrganizationModel{}
	for _, r := range records {
		known[r.Name] = r
	}
	models, err := s.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	creds, err := s.Credentials.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range models {
		if m.Name == name {
			return nil, entities.ErrConflict
		}
	}
	owned := map[string]bool{}
	for _, c := range creds {
		if c.Status == entities.StatusActive && (actor.Type == entities.PrincipalMaster && c.OwnerUserID == "" && c.OwnerTenantID == nil || actor.Type == entities.PrincipalUser && c.OwnerUserID == actor.UserID) {
			owned[c.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if seen[target] || target == name || strings.Contains(target, "/g/") || strings.HasSuffix(target, "/auto") {
			return nil, ErrInvalid
		}
		seen[target] = true
		if prior, ok := known[target]; ok {
			if kind != "group" || prior.Kind != "alias" || !prior.Enabled {
				return nil, ErrInvalid
			}
			continue
		}
		if kind == "group" {
			return nil, ErrInvalid
		}
		found := false
		for _, m := range models {
			if m.Name == target && m.Enabled {
				for _, r := range m.Routes {
					if r.Enabled && owned[r.CredentialID] {
						found = true
					}
				}
			}
		}
		if !found {
			return nil, ErrForbidden
		}
	}
	now := time.Now().UTC()
	record := entities.OrganizationModel{OrganizationID: orgID, Name: name, Kind: kind, Targets: append([]string(nil), targets...), SourceOwnerID: actor.UserID, Enabled: enabled, WeeklyLimitUSD: limit, CreatedAt: now, UpdatedAt: now}
	if old, ok := known[name]; ok {
		record.CreatedAt = old.CreatedAt
		if old.SourceOwnerID != record.SourceOwnerID && actor.Type != entities.PrincipalMaster {
			return nil, ErrForbidden
		}
	}
	if err = s.Repo.Put(ctx, record); err != nil {
		return nil, err
	}
	if s.Audit != nil {
		if err = s.Audit.AppendAudit(ctx, entities.AuditEvent{ID: entities.NewID("audit"), TS: now, ActorType: actor.Type, ActorID: actor.UserID, ActorLabel: actor.Username, OrganizationID: orgID, Action: "organization.model.publish", TargetType: "organization_model", TargetID: name, SafeMetadata: map[string]string{"kind": kind}}); err != nil {
			return nil, err
		}
	}
	return &record, nil
}
func (s *Service) Assign(ctx context.Context, actor entities.Principal, orgID, name, userID string, limit *float64, enabled bool) (*entities.OrganizationModelGrant, error) {
	if _, err := s.Manage(ctx, actor, orgID); err != nil {
		return nil, err
	}
	return s.assign(ctx, actor, orgID, name, userID, limit, enabled)
}
func (s *Service) assign(ctx context.Context, actor entities.Principal, orgID, name, userID string, limit *float64, enabled bool) (*entities.OrganizationModelGrant, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !validLimit(limit) {
		return nil, ErrInvalid
	}
	user, err := s.Identity.UserByID(ctx, userID)
	if err != nil || user.Status != entities.StatusActive {
		return nil, ErrForbidden
	}
	if _, personal := personalOwner(orgID); !personal {
		if _, err = s.Identity.Membership(ctx, orgID, userID); err != nil {
			return nil, ErrForbidden
		}
	}
	offers, err := s.Repo.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, o := range offers {
		found = found || o.Name == name && o.Kind == "alias"
	}
	if !found {
		return nil, entities.ErrNotFound
	}
	zero := 0.0
	if limit == nil {
		limit = &zero
	}
	grant := entities.OrganizationModelGrant{OrganizationID: orgID, Model: name, UserID: userID, Enabled: enabled, WeeklyLimitUSD: limit, UpdatedAt: time.Now().UTC()}
	if err = s.Repo.PutGrant(ctx, grant); err != nil {
		return nil, err
	}
	if s.Audit != nil {
		if err = s.Audit.AppendAudit(ctx, entities.AuditEvent{ID: entities.NewID("audit"), TS: grant.UpdatedAt, ActorType: actor.Type, ActorID: actor.UserID, ActorLabel: actor.Username, OrganizationID: orgID, Action: "organization.model.assign", TargetType: "organization_model_grant", TargetID: userID, SafeMetadata: map[string]string{"model": name}}); err != nil {
			return nil, err
		}
	}
	return &grant, nil
}

type Route struct {
	Model   entities.ModelDef
	Route   entities.ModelRoute
	Charges []entities.ModelBudgetCharge
}
type Resolution struct {
	Name           string
	OrganizationID string
	Routes         []Route
	Granted        bool
	PersonalAlias  bool
	SourceName     string
}

func (s *Service) Available(ctx context.Context, userID string) ([]Resolution, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	user, err := s.Identity.UserByID(ctx, userID)
	if err != nil || user.Status != entities.StatusActive {
		return nil, ErrForbidden
	}
	models, err := s.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	creds, err := s.Credentials.List(ctx)
	if err != nil {
		return nil, err
	}
	grants, err := s.Repo.Grants(ctx, "", userID)
	if err != nil {
		return nil, err
	}
	own, err := s.Repo.List(ctx, personalScope(userID))
	if err != nil {
		return nil, err
	}
	selfLimits, err := s.Repo.Grants(ctx, "self:"+userID, userID)
	if err != nil {
		return nil, err
	}
	type offerGrant struct {
		offer    entities.OrganizationModel
		grant    *entities.OrganizationModelGrant
		personal bool
	}
	candidates := []offerGrant{}
	for _, o := range own {
		candidates = append(candidates, offerGrant{offer: o, personal: true})
	}
	for i := range grants {
		g := &grants[i]
		if !g.Enabled || strings.HasPrefix(g.OrganizationID, "self:") {
			continue
		}
		if owner, personal := personalOwner(g.OrganizationID); personal {
			if owner == userID {
				continue
			}
		} else {
			org, err := s.Identity.OrganizationByID(ctx, g.OrganizationID)
			if err != nil || org.Status != entities.StatusActive {
				continue
			}
			if _, err = s.Identity.Membership(ctx, g.OrganizationID, userID); err != nil {
				continue
			}
		}
		offers, err := s.Repo.List(ctx, g.OrganizationID)
		if err != nil {
			return nil, err
		}
		for _, o := range offers {
			if o.Name == g.Model {
				candidates = append(candidates, offerGrant{offer: o, grant: g})
			}
		}
	}
	out := []Resolution{}
	for _, candidate := range candidates {
		o := candidate.offer
		if !o.Enabled || o.Kind != "alias" || len(o.Targets) != 1 {
			continue
		}
		if o.SourceOwnerID != "" {
			owner, err := s.Identity.UserByID(ctx, o.SourceOwnerID)
			if err != nil || owner.Status != entities.StatusActive {
				continue
			}
			if _, personal := personalOwner(o.OrganizationID); !personal {
				m, err := s.Identity.Membership(ctx, o.OrganizationID, o.SourceOwnerID)
				if err != nil || m.Role != entities.MembershipAdmin {
					continue
				}
			}
		}
		result := Resolution{Name: o.Name, OrganizationID: o.OrganizationID, Granted: candidate.grant != nil, PersonalAlias: candidate.personal, SourceName: o.Targets[0]}
		charges := []entities.ModelBudgetCharge{}
		if candidate.grant != nil {
			charges = append(charges, entities.ModelBudgetCharge{Scope: "user:" + userID + ":" + o.Name, LimitUSD: budgetValue(candidate.grant.WeeklyLimitUSD)})
			ownLimit := 0.0
			for _, g := range selfLimits {
				if g.Model == o.Name && g.Enabled && g.WeeklyLimitUSD != nil {
					ownLimit = *g.WeeklyLimitUSD
				}
			}
			charges = append(charges, entities.ModelBudgetCharge{Scope: "self:" + userID + ":" + o.Name, LimitUSD: ownLimit})
		}
		for _, model := range models {
			if model.Name != o.Targets[0] || !model.Enabled {
				continue
			}
			for _, r := range model.Routes {
				if !r.Enabled {
					continue
				}
				for _, c := range creds {
					if c.ID == r.CredentialID && c.Status == entities.StatusActive && c.OwnerUserID == o.SourceOwnerID && (o.SourceOwnerID != "" || c.OwnerTenantID == nil) {
						result.Routes = append(result.Routes, Route{Model: model, Route: r, Charges: charges})
					}
				}
			}
		}
		if len(result.Routes) > 512 {
			return nil, ErrInvalid
		}
		if len(result.Routes) > 0 {
			out = append(out, result)
		}
	}
	return out, nil
}
func (s *Service) Reserve(ctx context.Context, userID string, res Resolution, route Route, estimate float64, at time.Time) (*entities.ModelBudgetReservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if len(route.Charges) == 0 {
		return nil, nil
	}
	start, end, _, err := quota.Window("week", at)
	if err != nil {
		return nil, err
	}
	hold := entities.ModelBudgetReservation{ID: entities.NewID("budget"), OrganizationID: res.OrganizationID, UserID: userID, Model: res.Name, WindowStart: start, WindowEnd: end, Charges: route.Charges, AmountUSD: estimate, CreatedAt: at.UTC()}
	if err = s.Repo.Reserve(ctx, hold); err != nil {
		return nil, err
	}
	return &hold, nil
}
func (s *Service) Settle(ctx context.Context, hold *entities.ModelBudgetReservation, actual float64) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if hold == nil {
		return nil
	}
	return s.Repo.Settle(ctx, hold.OrganizationID, hold.ID, actual)
}

// CheckBudget checks all parent/alias/user limits in a single serialized operation.
func CheckBudget(hold entities.ModelBudgetReservation, prior []entities.ModelBudgetReservation) error {
	if hold.AmountUSD < 0 || math.IsNaN(hold.AmountUSD) || math.IsInf(hold.AmountUSD, 0) {
		return ErrInvalid
	}
	for _, existing := range prior {
		if existing.ID == hold.ID {
			return nil
		}
	}
	for _, charge := range hold.Charges {
		if charge.LimitUSD <= 0 {
			continue
		}
		spent := 0.0
		for _, existing := range prior {
			if !existing.WindowStart.Equal(hold.WindowStart) {
				continue
			}
			for _, c := range existing.Charges {
				if c.Scope == charge.Scope {
					spent += existing.AmountUSD
					break
				}
			}
		}
		if spent >= charge.LimitUSD || spent+hold.AmountUSD > charge.LimitUSD {
			return ErrBudget
		}
	}
	return nil
}

// Unlimited scopes are still recorded, so imposing a limit later cannot erase
// spend already incurred during the current week. Zero means unlimited.
func budgetValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
