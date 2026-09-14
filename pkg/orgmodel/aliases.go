package orgmodel

import (
	"context"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/provider"
)

func personalScope(userID string) string { return "user:" + userID }
func personalOwner(scope string) (string, bool) {
	owner, ok := strings.CutPrefix(scope, "user:")
	return owner, ok && owner != ""
}

// CheckAliasWrite is called under a repository-wide namespace lock. Aliases are
// one-to-one within their owner; public names cannot collide across owners.
func CheckAliasWrite(next entities.OrganizationModel, all []entities.OrganizationModel) error {
	for _, old := range all {
		if old.Name == next.Name {
			if old.OrganizationID != next.OrganizationID || old.Kind != next.Kind {
				return entities.ErrConflict
			}
			if next.Kind == "alias" && (len(old.Targets) != 1 || len(next.Targets) != 1 || old.Targets[0] != next.Targets[0]) {
				return entities.ErrConflict
			}
		}
		if old.OrganizationID == next.OrganizationID && old.Kind == "alias" && next.Kind == "alias" && old.Name != next.Name && len(old.Targets) == 1 && len(next.Targets) == 1 && old.Targets[0] == next.Targets[0] {
			return entities.ErrConflict
		}
	}
	return nil
}

// PersonalAliases operates on the authenticated user, not a supplied user ID.
func (s *Service) PersonalAliases(ctx context.Context, actor entities.Principal) ([]entities.OrganizationModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if actor.Type != entities.PrincipalUser || !actor.HasScope(entities.ScopeModelsManage) {
		return nil, ErrForbidden
	}
	return s.Repo.List(ctx, personalScope(actor.UserID))
}
func (s *Service) PublishPersonal(ctx context.Context, actor entities.Principal, slug, target string, enabled bool) (*entities.OrganizationModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if actor.Type != entities.PrincipalUser || !actor.HasScope(entities.ScopeModelsManage) {
		return nil, ErrForbidden
	}
	user, err := s.Identity.UserByID(ctx, actor.UserID)
	if err != nil || user.Status != entities.StatusActive {
		return nil, ErrForbidden
	}
	if !validName(slug) || strings.Contains(target, "/g/") || strings.HasSuffix(target, "/auto") {
		return nil, ErrInvalid
	}
	if err = s.ownsSource(ctx, actor, target); err != nil {
		return nil, err
	}
	namespace := provider.UserAliasNamespace(user.Username)
	if namespace == "" || namespace == "org" {
		return nil, ErrInvalid
	}
	// Avoid collision with existing provider/raw namespaces (e.g. cx/model).
	for _, def := range provider.Catalog() {
		if def.ModelPrefix == namespace {
			return nil, ErrInvalid
		}
	}
	models, err := s.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range models {
		if m.Name == provider.UserAliasID(user.Username, slug) {
			return nil, entities.ErrConflict
		}
	}
	now := time.Now().UTC()
	v := entities.OrganizationModel{OrganizationID: personalScope(user.ID), Name: provider.UserAliasID(user.Username, slug), Kind: "alias", Targets: []string{target}, SourceOwnerID: user.ID, Enabled: enabled, CreatedAt: now, UpdatedAt: now}
	if err = s.Repo.Put(ctx, v); err != nil {
		return nil, err
	}
	if s.Audit != nil {
		if err = s.Audit.AppendAudit(ctx, entities.AuditEvent{ID: entities.NewID("audit"), TS: now, ActorType: actor.Type, ActorID: actor.UserID, ActorLabel: actor.Username, Action: "model.alias.publish", TargetType: "model_alias", TargetID: v.Name, SafeMetadata: map[string]string{"source": target}}); err != nil {
			return nil, err
		}
	}
	return &v, nil
}
func (s *Service) ownsSource(ctx context.Context, actor entities.Principal, target string) error {
	models, err := s.Models.List(ctx)
	if err != nil {
		return err
	}
	creds, err := s.Credentials.List(ctx)
	if err != nil {
		return err
	}
	for _, m := range models {
		if m.Name != target || !m.Enabled {
			continue
		}
		for _, r := range m.Routes {
			if !r.Enabled {
				continue
			}
			for _, c := range creds {
				if c.ID == r.CredentialID && c.Status == entities.StatusActive && (actor.Type == entities.PrincipalUser && c.OwnerUserID == actor.UserID || actor.Type == entities.PrincipalMaster && c.OwnerUserID == "" && c.OwnerTenantID == nil) {
					return nil
				}
			}
		}
	}
	return ErrForbidden
}

// AssignPackage snapshots a group to direct per-model grants. Groups never
// participate in inference, listings, or spending. Existing grants keep their
// individual limits unless explicitly edited through Assign.
func (s *Service) AssignPackage(ctx context.Context, actor entities.Principal, orgID, name, userID string, limit *float64, enabled bool) ([]entities.OrganizationModelGrant, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !validLimit(limit) {
		return nil, ErrInvalid
	}
	if _, err := s.Manage(ctx, actor, orgID); err != nil {
		return nil, err
	}
	all, err := s.Repo.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var selected *entities.OrganizationModel
	for i := range all {
		if all[i].Name == name {
			selected = &all[i]
			break
		}
	}
	if selected == nil {
		return nil, entities.ErrNotFound
	}
	if selected.Kind == "group" && !selected.Enabled {
		return nil, ErrForbidden
	}
	if selected.Kind != "group" {
		v, err := s.Assign(ctx, actor, orgID, name, userID, limit, enabled)
		if err != nil {
			return nil, err
		}
		return []entities.OrganizationModelGrant{*v}, nil
	}
	existing, err := s.Repo.Grants(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}
	out := []entities.OrganizationModelGrant{}
	for _, target := range selected.Targets {
		perModel := limit
		for _, g := range existing {
			if g.Model == target {
				perModel = g.WeeklyLimitUSD
			}
		}
		v, err := s.Assign(ctx, actor, orgID, target, userID, perModel, enabled)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}
func (s *Service) AssignPersonal(ctx context.Context, actor entities.Principal, name, userID string, limit *float64, enabled bool) (*entities.OrganizationModelGrant, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if actor.Type != entities.PrincipalUser || !actor.HasScope(entities.ScopeModelsManage) {
		return nil, ErrForbidden
	}
	return s.assign(ctx, actor, personalScope(actor.UserID), name, userID, limit, enabled)
}

// SetSelfLimit cannot modify the grantor's record. Zero removes only this
// optional personal cap; the assigner's independent cap remains enforced.
func (s *Service) SetSelfLimit(ctx context.Context, actor entities.Principal, name string, limit *float64) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if actor.Type != entities.PrincipalUser || !actor.HasScope(entities.ScopeChat) || !validLimit(limit) {
		return ErrForbidden
	}
	resolutions, err := s.Available(ctx, actor.UserID)
	if err != nil {
		return err
	}
	for _, r := range resolutions {
		if r.Name == name && r.Granted {
			zero := 0.0
			if limit == nil {
				limit = &zero
			}
			return s.Repo.PutGrant(ctx, entities.OrganizationModelGrant{OrganizationID: "self:" + actor.UserID, Model: name, UserID: actor.UserID, Enabled: true, WeeklyLimitUSD: limit, UpdatedAt: time.Now().UTC()})
		}
	}
	return entities.ErrNotFound
}
