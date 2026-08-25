package usage

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type Repository interface {
	MonthSpendForKey(ctx context.Context, apiKeyID string) (float64, error)
	Summary(ctx context.Context, since time.Time) (*entities.UsageSummary, error)
	Recent(ctx context.Context, limit int) ([]entities.RecentEvent, error)
	SummaryForTenant(ctx context.Context, tenantID string, since time.Time) (*entities.UsageSummary, error)
	RecentForTenant(ctx context.Context, tenantID string, limit int) ([]entities.RecentEvent, error)
	InsertBatch(ctx context.Context, events []entities.UsageEvent) error
}

type Service struct {
	repo    Repository
	ch      chan entities.UsageEvent
	stop    chan struct{}
	force   chan struct{}
	done    chan struct{}
	pending *Pending
	close   sync.Once
	forcing sync.Once
}

var ErrClosed = errors.New("usage service closed")

func NewService(repo Repository, buffer int, pending *Pending) *Service {
	if buffer <= 0 {
		buffer = 1024
	}
	s := &Service{repo: repo, ch: make(chan entities.UsageEvent, buffer), stop: make(chan struct{}), force: make(chan struct{}), done: make(chan struct{}), pending: pending}
	go s.run()
	return s
}

func (s *Service) Record(ev entities.UsageEvent) {
	_ = s.RecordContext(context.Background(), ev)
}

// RecordContext applies backpressure instead of silently dropping billable
// usage when the buffer is full.
func (s *Service) RecordContext(ctx context.Context, ev entities.UsageEvent) error {
	if s.pending != nil {
		s.pending.Add(ev.ApiKeyID, ev.CostUSD)
	}
	select {
	case s.ch <- ev:
		return nil
	case <-s.stop:
		if s.pending != nil {
			s.pending.Sub(ev.ApiKeyID, ev.CostUSD)
		}
		return ErrClosed
	case <-ctx.Done():
		if s.pending != nil {
			s.pending.Sub(ev.ApiKeyID, ev.CostUSD)
		}
		return ctx.Err()
	}
}

func (s *Service) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.CloseContext(ctx)
}

func (s *Service) CloseContext(ctx context.Context) error {
	s.close.Do(func() { close(s.stop) })
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.forcing.Do(func() { close(s.force) })
		<-s.done
		return ctx.Err()
	}
}

func (s *Service) run() {
	defer close(s.done)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]entities.UsageEvent, 0, 256)
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.repo.InsertBatch(ctx, batch)
		cancel()
		if err != nil {
			return false
		}
		if s.pending != nil {
			for _, ev := range batch {
				s.pending.Sub(ev.ApiKeyID, ev.CostUSD)
			}
		}
		batch = batch[:0]
		return true
	}
	for {
		select {
		case ev := <-s.ch:
			batch = append(batch, ev)
			if len(batch) >= 256 {
				_ = flush()
			}
		case <-ticker.C:
			_ = flush()
		case <-s.stop:
			for {
				select {
				case ev := <-s.ch:
					batch = append(batch, ev)
					continue
				default:
				}
				break
			}
			for len(batch) > 0 && !flush() {
				select {
				case <-s.force:
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
			return
		}
	}
}

func (s *Service) MonthSpendForKey(ctx context.Context, apiKeyID string) (float64, error) {
	dbSpent, err := s.repo.MonthSpendForKey(ctx, apiKeyID)
	if err != nil {
		return 0, err
	}
	if s.pending != nil {
		return dbSpent + s.pending.Load(apiKeyID), nil
	}
	return dbSpent, nil
}

func (s *Service) Summary(ctx context.Context, since time.Time) (*entities.UsageSummary, error) {
	return s.repo.Summary(ctx, since)
}

func (s *Service) Recent(ctx context.Context, limit int) ([]entities.RecentEvent, error) {
	return s.repo.Recent(ctx, limit)
}

func (s *Service) SummaryForTenant(ctx context.Context, tenantID string, since time.Time) (*entities.UsageSummary, error) {
	return s.repo.SummaryForTenant(ctx, tenantID, since)
}

func (s *Service) RecentForTenant(ctx context.Context, tenantID string, limit int) ([]entities.RecentEvent, error) {
	return s.repo.RecentForTenant(ctx, tenantID, limit)
}
