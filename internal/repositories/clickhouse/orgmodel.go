package clickhouse

import (
	"context"
	"math"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/orgmodel"
)

type OrganizationModelRepo struct{ s *Store }

func NewOrganizationModelRepo(s *Store) *OrganizationModelRepo { return &OrganizationModelRepo{s} }
func (r *OrganizationModelRepo) List(ctx context.Context, org string) ([]entities.OrganizationModel, error) {
	return list[entities.OrganizationModel](ctx, r.s, "org-model:"+org)
}
func (r *OrganizationModelRepo) Put(ctx context.Context, v entities.OrganizationModel) error {
	return r.s.put(ctx, "org-model:"+v.OrganizationID, v.Name, v)
}
func (r *OrganizationModelRepo) Grants(ctx context.Context, org, user string) ([]entities.OrganizationModelGrant, error) {
	all, err := list[entities.OrganizationModelGrant](ctx, r.s, "org-grant:"+org)
	out := []entities.OrganizationModelGrant{}
	for _, g := range all {
		if user == "" || g.UserID == user {
			out = append(out, g)
		}
	}
	return out, err
}
func (r *OrganizationModelRepo) PutGrant(ctx context.Context, v entities.OrganizationModelGrant) error {
	return r.s.put(ctx, "org-grant:"+v.OrganizationID, v.UserID+":"+v.Model, v)
}
func (r *OrganizationModelRepo) Reserve(ctx context.Context, hold entities.ModelBudgetReservation) error {
	return r.s.budgetMutation(ctx, hold.OrganizationID, func() error {
		prior, err := list[entities.ModelBudgetReservation](ctx, r.s, "org-budget:"+hold.OrganizationID)
		if err != nil {
			return err
		}
		for _, p := range prior {
			if p.ID == hold.ID {
				return nil
			}
		}
		if err = orgmodel.CheckBudget(hold, prior); err != nil {
			return err
		}
		return r.s.put(ctx, "org-budget:"+hold.OrganizationID, hold.ID, hold)
	})
}
func (r *OrganizationModelRepo) Settle(ctx context.Context, org, id string, actual float64) error {
	if actual < 0 || math.IsNaN(actual) || math.IsInf(actual, 0) {
		return orgmodel.ErrInvalid
	}
	return r.s.budgetMutation(ctx, org, func() error {
		hold, err := get[entities.ModelBudgetReservation](ctx, r.s, "org-budget:"+org, id)
		if err != nil {
			return err
		}
		if hold.Settled {
			return nil
		}
		hold.Settled = true
		hold.AmountUSD = actual
		return r.s.put(ctx, "org-budget:"+org, id, hold)
	})
}
