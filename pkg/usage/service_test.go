package usage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type testRepo struct {
	mu       sync.Mutex
	failures int
	events   []entities.UsageEvent
}

func (r *testRepo) InsertBatch(_ context.Context, events []entities.UsageEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures > 0 {
		r.failures--
		return errors.New("temporary failure")
	}
	r.events = append(r.events, events...)
	return nil
}
func (*testRepo) MonthSpendForKey(context.Context, string) (float64, error)          { return 2, nil }
func (*testRepo) Summary(context.Context, time.Time) (*entities.UsageSummary, error) { return nil, nil }
func (*testRepo) Recent(context.Context, int) ([]entities.RecentEvent, error)        { return nil, nil }
func (*testRepo) SummaryForTenant(context.Context, string, time.Time) (*entities.UsageSummary, error) {
	return nil, nil
}
func (*testRepo) RecentForTenant(context.Context, string, int) ([]entities.RecentEvent, error) {
	return nil, nil
}

func TestServiceRetriesAndTracksPending(t *testing.T) {
	repo := &testRepo{failures: 1}
	pending := NewPending()
	svc := NewService(repo, 1, pending)
	ev := entities.UsageEvent{ApiKeyID: "key", CostUSD: 0.75}
	if err := svc.RecordContext(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	spent, err := svc.MonthSpendForKey(context.Background(), "key")
	if err != nil || spent != 2.75 {
		t.Fatalf("pending spend missing: spent=%v err=%v", spent, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && pending.Load("key") != 0 {
		time.Sleep(25 * time.Millisecond)
	}
	if got := pending.Load("key"); got != 0 {
		t.Fatalf("pending cost not cleared after durable retry: %v", got)
	}
	svc.Close()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.events) != 1 {
		t.Fatalf("want one durable event, got %d", len(repo.events))
	}
}
