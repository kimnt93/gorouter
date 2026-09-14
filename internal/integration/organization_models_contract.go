package integration

import (
	"context"
	"errors"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/orgmodel"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func RunOrganizationModelsContract(t *testing.T, repo orgmodel.Repository) {
	t.Helper()
	ctx := context.Background()
	org := entities.NewID("org")
	at := time.Now().UTC()
	limit := 1.0
	offer := entities.OrganizationModel{OrganizationID: org, Name: "org/" + org + "/g/default", Kind: "group", Targets: []string{"example/small", "cx/model"}, Enabled: true, WeeklyLimitUSD: &limit, CreatedAt: at, UpdatedAt: at}
	if err := repo.Put(ctx, offer); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.List(ctx, org)
	if err != nil || len(loaded) != 1 || len(loaded[0].Targets) != 2 {
		t.Fatalf("list=%+v err=%v", loaded, err)
	}
	grant := entities.OrganizationModelGrant{OrganizationID: org, Model: offer.Name, UserID: "u", Enabled: true, WeeklyLimitUSD: &limit, UpdatedAt: at}
	if err := repo.PutGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	grants, err := repo.Grants(ctx, org, "foreign")
	if err != nil || len(grants) != 0 {
		t.Fatal("foreign grants visible")
	}
	start := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	hold := entities.ModelBudgetReservation{OrganizationID: org, UserID: "u", Model: offer.Name, WindowStart: start, WindowEnd: start.AddDate(0, 0, 7), AmountUSD: .6, Charges: []entities.ModelBudgetCharge{{Scope: "group", LimitUSD: 1}, {Scope: "user:u", LimitUSD: 1}}, CreatedAt: at}
	var accepted atomic.Int32
	var mu sync.Mutex
	var ids []string
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := hold
			h.ID = entities.NewID("budget")
			err := repo.Reserve(ctx, h)
			if err == nil {
				accepted.Add(1)
				mu.Lock()
				ids = append(ids, h.ID)
				mu.Unlock()
			} else if !errors.Is(err, orgmodel.ErrBudget) {
				t.Errorf("reserve: %v", err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("concurrent admission=%d", accepted.Load())
	}
	if err = repo.Settle(ctx, org, ids[0], .2); err != nil {
		t.Fatal(err)
	}
	if err = repo.Settle(ctx, org, ids[0], .9); err != nil {
		t.Fatal(err)
	}
	hold.ID = entities.NewID("budget")
	if err = repo.Reserve(ctx, hold); err != nil {
		t.Fatal(err)
	} // idempotent settlement retained .2, not .9
	if err = repo.Reserve(ctx, hold); err != nil {
		t.Fatal(err)
	}
	// Adding a limit after unlimited operation must include prior recorded spend.
	unlimited := hold
	unlimited.ID = entities.NewID("budget")
	unlimited.Charges = []entities.ModelBudgetCharge{{Scope: "initially-unlimited", LimitUSD: -1}}
	unlimited.AmountUSD = 2
	if err = repo.Reserve(ctx, unlimited); err != nil {
		t.Fatal(err)
	}
	unlimited.ID = entities.NewID("budget")
	unlimited.Charges[0].LimitUSD = 1
	if err = repo.Reserve(ctx, unlimited); !errors.Is(err, orgmodel.ErrBudget) {
		t.Fatalf("new limit omitted previous spend: %v", err)
	}
	// Old-window unsettled charges stay in their admission window, not the next.
	hold.ID = entities.NewID("budget")
	hold.WindowStart = hold.WindowEnd
	hold.WindowEnd = hold.WindowEnd.AddDate(0, 0, 7)
	if err = repo.Reserve(ctx, hold); err != nil {
		t.Fatal(err)
	}
}

type PrimaryKeyRepo interface {
	CreatePrimary(context.Context, entities.ApiKey) (*entities.ApiKey, error)
	Rotate(context.Context, string) (*entities.ApiKey, error)
}

func RunPrimaryKeyContract(t *testing.T, repo PrimaryKeyRepo, userID string) {
	ctx := context.Background()
	var accepted atomic.Int32
	var key *entities.ApiKey
	var lock sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := repo.CreatePrimary(ctx, entities.ApiKey{Name: "primary", OwnerType: entities.OwnerUser, OwnerUserID: userID, QuotaPeriod: entities.QuotaPeriodNone})
			if err == nil {
				accepted.Add(1)
				lock.Lock()
				key = v
				lock.Unlock()
			} else if !errors.Is(err, entities.ErrConflict) {
				t.Errorf("primary create: %v", err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("created primary keys=%d", accepted.Load())
	}
	rotated, err := repo.Rotate(ctx, key.ID)
	if err != nil || rotated.ID != key.ID || rotated.OwnerUserID != userID {
		t.Fatalf("rotation=%v", err)
	}
	if _, err = repo.CreatePrimary(ctx, entities.ApiKey{Name: "second", OwnerType: entities.OwnerUser, OwnerUserID: userID}); !errors.Is(err, entities.ErrConflict) {
		t.Fatalf("second key accepted: %v", err)
	}
}

func RunAliasUniquenessContract(t *testing.T, repo orgmodel.Repository) {
	ctx := context.Background()
	owner := entities.NewID("alias-owner")
	at := time.Now().UTC()
	base := entities.OrganizationModel{OrganizationID: owner, Name: owner + "/a", Kind: "alias", Targets: []string{"cx/model"}, Enabled: true, CreatedAt: at, UpdatedAt: at}
	if err := repo.Put(ctx, base); err != nil {
		t.Fatal(err)
	}
	second := base
	second.Name = owner + "/b"
	if err := repo.Put(ctx, second); !errors.Is(err, entities.ErrConflict) {
		t.Fatalf("second alias for source: %v", err)
	}
	second = base
	second.Targets = []string{"cx/different"}
	if err := repo.Put(ctx, second); !errors.Is(err, entities.ErrConflict) {
		t.Fatalf("retargeted same alias: %v", err)
	}
	second = base
	second.OrganizationID = "foreign"
	if err := repo.Put(ctx, second); !errors.Is(err, entities.ErrConflict) {
		t.Fatalf("foreign public name: %v", err)
	}
	// Under concurrent publication, only one name can win for another source.
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := base
			v.Targets = []string{"cx/raced"}
			v.Name = entities.NewID("alias")
			err := repo.Put(ctx, v)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, entities.ErrConflict) {
				t.Errorf("alias race: %v", err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("alias publication winners=%d", accepted.Load())
	}
	// Default zero budget must allow work, but imposing a cap later includes spend.
	hold := entities.ModelBudgetReservation{ID: entities.NewID("hold"), OrganizationID: owner, UserID: "a", WindowStart: at.Truncate(24 * time.Hour), WindowEnd: at.AddDate(0, 0, 7), Charges: []entities.ModelBudgetCharge{{Scope: "assigned:a", LimitUSD: 0}}, AmountUSD: 10, CreatedAt: at}
	if err := repo.Reserve(ctx, hold); err != nil {
		t.Fatal(err)
	}
	hold.ID = entities.NewID("hold")
	hold.Charges[0].LimitUSD = 5
	if err := repo.Reserve(ctx, hold); !errors.Is(err, orgmodel.ErrBudget) {
		t.Fatal("adding cap reset spend")
	}
	hold.ID = entities.NewID("hold")
	hold.UserID = "b"
	hold.Charges[0] = entities.ModelBudgetCharge{Scope: "assigned:b", LimitUSD: 50}
	if err := repo.Reserve(ctx, hold); err != nil {
		t.Fatalf("other user's budget affected: %v", err)
	}
}
