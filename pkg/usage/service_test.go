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
	since    time.Time
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
func (*testRepo) MonthSpendForKey(context.Context, string) (float64, error) { return 2, nil }
func (r *testRepo) SpendForKeySince(_ context.Context, _ string, since time.Time) (float64, error) {
	r.since = since
	return 3, nil
}
func (*testRepo) Summary(context.Context, time.Time) (*entities.UsageSummary, error) { return nil, nil }
func (*testRepo) Recent(context.Context, int) ([]entities.RecentEvent, error)        { return nil, nil }
func (*testRepo) SummaryForTenant(context.Context, string, time.Time) (*entities.UsageSummary, error) {
	return nil, nil
}

func TestSpendForKeySinceIncludesPendingAndPassesUTCWindow(t *testing.T) {
	repo := &testRepo{}
	pending := NewPending()
	pending.Add("key", 0.25)
	svc := NewService(repo, 1, pending)
	t.Cleanup(svc.Close)
	since := time.Date(2026, time.August, 24, 7, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	spent, err := svc.SpendForKeySince(context.Background(), "key", since)
	if err != nil || spent != 3.25 {
		t.Fatalf("spent=%v err=%v", spent, err)
	}
	if repo.since.Location() != time.UTC || !repo.since.Equal(since) {
		t.Fatalf("repository since=%v, want UTC equivalent of %v", repo.since, since)
	}
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
