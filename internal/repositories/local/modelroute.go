package local

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type ModelRouteRepo struct{ s *Store }

func NewModelRouteRepo(s *Store) *ModelRouteRepo { return &ModelRouteRepo{s} }
func (r *ModelRouteRepo) Upsert(ctx context.Context, m entities.ModelDef) error {
	if m.UpstreamModel == "" {
		m.UpstreamModel = m.Name
	}
	if m.Strategy == "" {
		m.Strategy = "priority"
	}
	for i := range m.Routes {
		if m.Routes[i].Weight <= 0 {
			m.Routes[i].Weight = 1
		}
	}
	return r.s.put(ctx, "model", m.Name, m)
}
func (r *ModelRouteRepo) Delete(ctx context.Context, name string) error {
	return r.s.del(ctx, "model", name)
}
func (r *ModelRouteRepo) List(ctx context.Context) ([]entities.ModelDef, error) {
	v, e := list[entities.ModelDef](ctx, r.s, "model")
	if e != nil {
		return nil, e
	}
	prices, _ := r.ListPrices(ctx)
	for i := range v {
		if p, ok := prices[v[i].Name]; ok {
			v[i].Price = &p
		}
	}
	sort.Slice(v, func(i, j int) bool { return v[i].Name < v[j].Name })
	return v, nil
}
func (r *ModelRouteRepo) SetPrice(ctx context.Context, model string, p entities.Price) error {
	return r.s.put(ctx, "price", model, p)
}
func (r *ModelRouteRepo) DeletePrice(ctx context.Context, model string) error {
	return r.s.del(ctx, "price", model)
}
func (r *ModelRouteRepo) ListPrices(ctx context.Context) (map[string]entities.Price, error) {
	rows, err := r.s.DB.QueryContext(ctx, `SELECT key,payload FROM config_records WHERE entity='price'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]entities.Price)
	for rows.Next() {
		var key string
		var payload []byte
		if err := rows.Scan(&key, &payload); err != nil {
			return nil, err
		}
		var price entities.Price
		if err := json.Unmarshal(payload, &price); err != nil {
			return nil, err
		}
		out[key] = price
	}
	return out, rows.Err()
}
func (r *ModelRouteRepo) ReplaceCatalogPrices(ctx context.Context, source string, prices []entities.CatalogPrice) error {
	old, e := r.ListCatalogPrices(ctx)
	if e != nil {
		return e
	}
	keep := map[string]bool{}
	for _, p := range prices {
		p.Source = source
		if p.UpdatedAt.IsZero() {
			p.UpdatedAt = time.Now().UTC()
		}
		keep[p.Model] = true
		if e = r.s.put(ctx, "catalog_price", p.Model, p); e != nil {
			return e
		}
	}
	for _, p := range old {
		if p.Source == source && !keep[p.Model] {
			if e = r.s.del(ctx, "catalog_price", p.Model); e != nil {
				return e
			}
		}
	}
	return nil
}
func (r *ModelRouteRepo) ListCatalogPrices(ctx context.Context) ([]entities.CatalogPrice, error) {
	v, e := list[entities.CatalogPrice](ctx, r.s, "catalog_price")
	sort.Slice(v, func(i, j int) bool { return strings.Compare(v[i].Model, v[j].Model) < 0 })
	return v, e
}
