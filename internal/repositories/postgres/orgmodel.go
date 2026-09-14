package postgres

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/orgmodel"
	"math"
	"time"
)

type OrganizationModelRepo struct{ db *DB }

func NewOrganizationModelRepo(db *DB) *OrganizationModelRepo { return &OrganizationModelRepo{db} }
func (r *OrganizationModelRepo) List(ctx context.Context, org string) ([]entities.OrganizationModel, error) {
	return orgRecords[entities.OrganizationModel](ctx, r.db, org, "model")
}
func (r *OrganizationModelRepo) Grants(ctx context.Context, org, user string) ([]entities.OrganizationModelGrant, error) {
	all, err := orgRecords[entities.OrganizationModelGrant](ctx, r.db, org, "grant")
	out := []entities.OrganizationModelGrant{}
	for _, g := range all {
		if user == "" || g.UserID == user {
			out = append(out, g)
		}
	}
	return out, err
}
func orgRecords[T any](ctx context.Context, db *DB, org, kind string) ([]T, error) {
	rows, err := db.Pool.Query(ctx, `SELECT payload FROM organization_model_records WHERE ($1='' OR organization_id=$1) AND kind=$2 ORDER BY id`, org, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var raw []byte
		var v T
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *OrganizationModelRepo) put(ctx context.Context, org, kind, id string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx, `INSERT INTO organization_model_records(organization_id,kind,id,payload) VALUES($1,$2,$3,$4) ON CONFLICT(organization_id,kind,id) DO UPDATE SET payload=EXCLUDED.payload`, org, kind, id, raw)
	return err
}
func (r *OrganizationModelRepo) Put(ctx context.Context, v entities.OrganizationModel) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('model-alias-namespace',0))`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT payload FROM organization_model_records WHERE kind='model'`)
	if err != nil {
		return err
	}
	prior := []entities.OrganizationModel{}
	for rows.Next() {
		var raw []byte
		var o entities.OrganizationModel
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &o); err != nil {
			rows.Close()
			return err
		}
		prior = append(prior, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = orgmodel.CheckAliasWrite(v, prior); err != nil {
		return err
	}
	for _, o := range prior {
		if o.Name == v.Name {
			v.CreatedAt = o.CreatedAt
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO organization_model_records(organization_id,kind,id,payload) VALUES($1,'model',$2,$3) ON CONFLICT(organization_id,kind,id) DO UPDATE SET payload=EXCLUDED.payload`, v.OrganizationID, v.Name, raw)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *OrganizationModelRepo) PutGrant(ctx context.Context, v entities.OrganizationModelGrant) error {
	return r.put(ctx, v.OrganizationID, "grant", v.UserID+":"+v.Model, v)
}
func (r *OrganizationModelRepo) Reserve(ctx context.Context, hold entities.ModelBudgetReservation) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "org-budget:"+hold.OrganizationID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT payload FROM organization_model_records WHERE organization_id=$1 AND kind='budget' AND payload->>'window_start'=$2`, hold.OrganizationID, hold.WindowStart.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	prior := []entities.ModelBudgetReservation{}
	for rows.Next() {
		var raw []byte
		var v entities.ModelBudgetReservation
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &v); err != nil {
			rows.Close()
			return err
		}
		prior = append(prior, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = orgmodel.CheckBudget(hold, prior); err != nil {
		return err
	}
	raw, _ := json.Marshal(hold)
	if _, err = tx.Exec(ctx, `INSERT INTO organization_model_records(organization_id,kind,id,payload) VALUES($1,'budget',$2,$3) ON CONFLICT DO NOTHING`, hold.OrganizationID, hold.ID, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *OrganizationModelRepo) Settle(ctx context.Context, org, id string, actual float64) error {
	if actual < 0 || math.IsNaN(actual) || math.IsInf(actual, 0) {
		return orgmodel.ErrInvalid
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "org-budget:"+org); err != nil {
		return err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT payload FROM organization_model_records WHERE organization_id=$1 AND kind='budget' AND id=$2 FOR UPDATE`, org, id).Scan(&raw); err == pgx.ErrNoRows {
		return entities.ErrNotFound
	} else if err != nil {
		return err
	}
	var hold entities.ModelBudgetReservation
	if err = json.Unmarshal(raw, &hold); err != nil {
		return err
	}
	if hold.Settled {
		return nil
	}
	hold.Settled = true
	hold.AmountUSD = actual
	raw, _ = json.Marshal(hold)
	if _, err = tx.Exec(ctx, `UPDATE organization_model_records SET payload=$3 WHERE organization_id=$1 AND kind='budget' AND id=$2`, org, id, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
