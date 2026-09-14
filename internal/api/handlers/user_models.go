package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/orgmodel"
)

// userModels materializes only personal routes and explicitly assigned org
// offers. No API-key mutation or extra agent credential is involved.
func (g *Gateway) userModels(ctx context.Context, key *GatewayAccessContext, models []entities.ModelDef) ([]entities.ModelDef, map[string]orgmodel.Resolution, error) {
	grants := map[string]orgmodel.Resolution{}
	if g.OrgModels == nil || key.Master || key.Actor.UserID == "" {
		return models, grants, nil
	}
	creds, err := g.Creds.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	owned := map[string]bool{}
	for _, c := range creds {
		if c.OwnerUserID == key.Actor.UserID && c.Status == entities.StatusActive {
			owned[c.ID] = true
		}
	}
	out := []entities.ModelDef{}
	for _, m := range models {
		routes := []entities.ModelRoute{}
		for _, r := range m.Routes {
			if owned[r.CredentialID] && r.Enabled {
				routes = append(routes, r)
			}
		}
		if len(routes) > 0 {
			m.Routes = routes
			out = append(out, m)
		}
	}
	available, err := g.OrgModels.Available(ctx, key.Actor.UserID)
	if err != nil {
		return nil, nil, err
	}
	for _, res := range available {
		m := entities.ModelDef{Name: res.Name, Strategy: "priority", Enabled: true}
		for i, r := range res.Routes {
			route := r.Route
			if route.UpstreamModel == "" {
				route.UpstreamModel = r.Model.UpstreamModel
			}
			route.Priority = len(res.Routes) - i
			m.Routes = append(m.Routes, route)
			if m.Metadata == nil {
				m.Metadata = r.Model.Metadata
				m.UpstreamModel = route.UpstreamModel
			}
		}
		out = append(out, m)
		grants[m.Name] = res
	}
	return out, grants, nil
}
func (g *Gateway) settleOrganization(ctx context.Context, key *GatewayAccessContext, cost float64) error {
	if g.OrgModels == nil || key.ModelBudget == nil {
		return nil
	}
	settleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := g.OrgModels.Settle(settleCtx, key.ModelBudget, cost); err != nil {
		return fmt.Errorf("organization settlement: %w", err)
	}
	key.ModelBudget = nil
	return nil
}
