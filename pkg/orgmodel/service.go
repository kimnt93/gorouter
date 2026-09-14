// Package orgmodel owns organization-published aliases, ordered model groups,
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
	if !validName(slug) || !validLimit(limit) || len(targets) == 0 || len(targets) > 32 || (kind != "alias" && kind != "group") || kind == "alias" && len(targets) != 1 {
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := s.Manage(ctx, actor, orgID); err != nil {
		return nil, err
	}
	if !validLimit(limit) {
		return nil, ErrInvalid
	}
	user, err := s.Identity.UserByID(ctx, userID)
	if err != nil || user.Status != entities.StatusActive {
		return nil, ErrForbidden
	}
	if _, err = s.Identity.Membership(ctx, orgID, userID); err != nil {
		return nil, ErrForbidden
	}
	offers, err := s.Repo.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, o := range offers {
		found = found || o.Name == name
	}
	if !found {
		return nil, entities.ErrNotFound
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
}

func (s *Service) Available(ctx context.Context, userID string) ([]Resolution, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	memberships, err := s.Identity.ListMembershipsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []Resolution{}
	models, err := s.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	creds, err := s.Credentials.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		org, err := s.Identity.OrganizationByID(ctx, membership.OrganizationID)
		if err != nil {
			return nil, err
		}
		if org.Status != entities.StatusActive {
			continue
		}
		offers, err := s.Repo.List(ctx, org.ID)
		if err != nil {
			return nil, err
		}
		byName := map[string]entities.OrganizationModel{}
		for _, o := range offers {
			byName[o.Name] = o
		}
		grants, err := s.Repo.Grants(ctx, org.ID, userID)
		if err != nil {
			return nil, err
		}
		for _, grant := range grants {
			if !grant.Enabled {
				continue
			}
			offer, ok := byName[grant.Model]
			if !ok || !offer.Enabled {
				continue
			}
			result := Resolution{Name: offer.Name, OrganizationID: org.ID}
			base := []entities.ModelBudgetCharge{{Scope: "user:" + userID + ":" + offer.Name, LimitUSD: budgetValue(grant.WeeklyLimitUSD)}}
			var expand func(entities.OrganizationModel, []entities.ModelBudgetCharge)
			expand = func(current entities.OrganizationModel, charges []entities.ModelBudgetCharge) {
				if current.SourceOwnerID != "" {
					owner, err := s.Identity.UserByID(ctx, current.SourceOwnerID)
					if err != nil || owner.Status != entities.StatusActive {
						return
					}
					member, err := s.Identity.Membership(ctx, current.OrganizationID, current.SourceOwnerID)
					if err != nil || member.Role != entities.MembershipAdmin {
						return
					}
				}

				charges = append([]entities.ModelBudgetCharge(nil), charges...)
				charges = append(charges, entities.ModelBudgetCharge{Scope: "model:" + current.Name, LimitUSD: budgetValue(current.WeeklyLimitUSD)})
				for _, target := range current.Targets {
					if alias, ok := byName[target]; ok {
						if current.Kind == "group" && alias.Kind == "alias" && alias.Enabled {
							expand(alias, charges)
						}
						continue
					}
					for _, model := range models {
						if model.Name != target || !model.Enabled {
							continue
						}
						for _, route := range model.Routes {
							if !route.Enabled {
								continue
							}
							for _, c := range creds {
								if c.ID == route.CredentialID && c.Status == entities.StatusActive && c.OwnerUserID == current.SourceOwnerID && (current.SourceOwnerID != "" || c.OwnerTenantID == nil) {
									result.Routes = append(result.Routes, Route{Model: model, Route: route, Charges: charges})
								}
							}
						}
					}
				}
			}
			expand(offer, base)
			if len(result.Routes) > 512 {
				return nil, ErrInvalid
			}
			if len(result.Routes) > 0 {
				out = append(out, result)
			}
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
		if charge.LimitUSD < 0 {
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
// spend already incurred during the current week. -1 is internal-only unlimited.
func budgetValue(value *float64) float64 {
	if value == nil {
		return -1
	}
	return *value
}
