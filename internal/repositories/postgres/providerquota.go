package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kimnt93/gorouter/pkg/providerquota"
)

type ProviderQuotaRepo struct{ db *DB }

func NewProviderQuotaRepo(db *DB) *ProviderQuotaRepo { return &ProviderQuotaRepo{db: db} }

func (r *ProviderQuotaRepo) LoadAll(ctx context.Context) ([]providerquota.Snapshot, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT credential_id,provider,account,plan,fetched_at,available,windows,message,in_use FROM provider_quota_snapshots ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []providerquota.Snapshot
	for rows.Next() {
		var snapshot providerquota.Snapshot
		var windows []byte
		if err := rows.Scan(&snapshot.CredentialID, &snapshot.Provider, &snapshot.Account, &snapshot.Plan, &snapshot.FetchedAt, &snapshot.Available, &windows, &snapshot.Message, &snapshot.InUse); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(windows, &snapshot.Windows); err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, rows.Err()
}

func (r *ProviderQuotaRepo) Save(ctx context.Context, snapshot providerquota.Snapshot) error {
	windows, err := json.Marshal(snapshot.Windows)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx, `INSERT INTO provider_quota_snapshots
		(credential_id,provider,account,plan,fetched_at,available,windows,message,in_use,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (credential_id) DO UPDATE SET provider=EXCLUDED.provider,account=EXCLUDED.account,
		plan=EXCLUDED.plan,fetched_at=EXCLUDED.fetched_at,available=EXCLUDED.available,
		windows=EXCLUDED.windows,message=EXCLUDED.message,in_use=EXCLUDED.in_use,updated_at=EXCLUDED.updated_at`,
		snapshot.CredentialID, snapshot.Provider, snapshot.Account, snapshot.Plan, snapshot.FetchedAt,
		snapshot.Available, windows, snapshot.Message, snapshot.InUse, time.Now().UTC())
	return err
}

func (r *ProviderQuotaRepo) SetInUse(ctx context.Context, credentialID, provider string) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE provider_quota_snapshots SET in_use=FALSE,updated_at=$1 WHERE provider=$2 AND in_use`, time.Now().UTC(), provider); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE provider_quota_snapshots SET in_use=TRUE,updated_at=$1 WHERE credential_id=$2`, time.Now().UTC(), credentialID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
