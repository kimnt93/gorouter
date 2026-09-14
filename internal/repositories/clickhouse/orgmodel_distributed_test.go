package clickhouse

import (
	"context"
	"errors"
	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/orgmodel"
	"github.com/redis/go-redis/v9"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOrganizationBudgetTwoReplicas(t *testing.T) {
	dsn, url := os.Getenv("TEST_CLICKHOUSE_URL"), os.Getenv("TEST_REDIS_URL")
	if dsn == "" || url == "" {
		t.Skip("requires isolated ClickHouse and Redis")
	}
	ctx := context.Background()
	db, err := database.ConnectClickHouse(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	opts, _ := redis.ParseURL(url)
	client := redis.NewClient(opts)
	defer client.Close()
	lock, _ := NewRedisMutationLocker(client, 15*time.Second)
	a, b := NewOrganizationModelRepo(NewWithLocker(db.Conn, lock)), NewOrganizationModelRepo(NewWithLocker(db.Conn, lock))
	org := entities.NewID("budget-test")
	now := time.Now().UTC()
	start := now.Truncate(24 * time.Hour)
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repo := a
			if i%2 == 1 {
				repo = b
			}
			err := repo.Reserve(ctx, entities.ModelBudgetReservation{ID: entities.NewID("hold"), OrganizationID: org, WindowStart: start, WindowEnd: start.AddDate(0, 0, 7), Charges: []entities.ModelBudgetCharge{{Scope: "group", LimitUSD: 1}}, AmountUSD: .6, CreatedAt: now})
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, orgmodel.ErrBudget) {
				t.Errorf("reserve: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted=%d", accepted.Load())
	}
	// Redis loss is fail-closed; durable spend is not replaced by an empty map.
	_ = client.Close()
	if err = a.Reserve(ctx, entities.ModelBudgetReservation{ID: entities.NewID("hold"), OrganizationID: org}); !errors.Is(err, ErrMutationLockUnavailable) {
		t.Fatalf("outage=%v", err)
	}
}
