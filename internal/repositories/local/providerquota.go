package local

import (
	"context"

	"github.com/kimnt93/gorouter/pkg/providerquota"
)

type ProviderQuotaRepo struct{ s *Store }

func NewProviderQuotaRepo(s *Store) *ProviderQuotaRepo { return &ProviderQuotaRepo{s: s} }

func (r *ProviderQuotaRepo) LoadAll(ctx context.Context) ([]providerquota.Snapshot, error) {
	return list[providerquota.Snapshot](ctx, r.s, "provider_quota")
}

func (r *ProviderQuotaRepo) Save(ctx context.Context, snapshot providerquota.Snapshot) error {
	return r.s.put(ctx, "provider_quota", snapshot.CredentialID, snapshot)
}

func (r *ProviderQuotaRepo) SetInUse(ctx context.Context, credentialID, provider string) error {
	return r.s.mutate(ctx, "provider-quota-in-use:"+provider, func() error {
		snapshots, err := r.LoadAll(ctx)
		if err != nil {
			return err
		}
		for _, snapshot := range snapshots {
			if snapshot.Provider != provider {
				continue
			}
			next := snapshot.CredentialID == credentialID
			if snapshot.InUse == next {
				continue
			}
			snapshot.InUse = next
			if err := r.Save(ctx, snapshot); err != nil {
				return err
			}
		}
		return nil
	})
}
