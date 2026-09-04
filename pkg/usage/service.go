package usage

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type Repository interface {
	SpendForKeySince(context.Context, string, time.Time) (float64, error)
	Summary(context.Context, time.Time) (*entities.UsageSummary, error)
	Recent(context.Context, int) ([]entities.RecentEvent, error)
	SummaryForTenant(context.Context, string, time.Time) (*entities.UsageSummary, error)
	RecentForTenant(context.Context, string, int) ([]entities.RecentEvent, error)
	InsertBatch(context.Context, []entities.UsageEvent) error
}

type principalRepository interface {
	entities.PrincipalUsageRepository
}

type Service struct {
	repo            Repository
	ch              chan entities.UsageEvent
	jobs            chan []entities.UsageEvent
	force           chan struct{}
	done            chan struct{}
	pending         *Pending
	mu              sync.RWMutex
	closed          bool
	closeOnce       sync.Once
	forceOnce       sync.Once
	workers         sync.WaitGroup
	conversationBox conversationSealer
}

var ErrClosed = errors.New("usage service closed")

func NewService(repo Repository, buffer int, pending *Pending) *Service {
	return NewServiceWithConcurrency(repo, buffer, 1, pending)
}

func NewServiceWithConcurrency(repo Repository, buffer, concurrency int, pending *Pending) *Service {
	if buffer <= 0 {
		buffer = 100000
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	s := &Service{repo: repo, ch: make(chan entities.UsageEvent, buffer), jobs: make(chan []entities.UsageEvent, concurrency*2), force: make(chan struct{}), done: make(chan struct{}), pending: pending}
	for i := 0; i < concurrency; i++ {
		s.workers.Add(1)
		go s.worker()
	}
	go s.dispatch()
	return s
}

func (s *Service) Record(ev entities.UsageEvent) { _ = s.RecordContext(context.Background(), ev) }

func (s *Service) RecordContext(ctx context.Context, ev entities.UsageEvent) error {
	if ev.ID == "" {
		ev.ID = entities.NewID("usage")
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	} else {
		ev.TS = ev.TS.UTC()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return ErrClosed
	}
	if s.pending != nil {
		s.pending.AddAt(ev.ApiKeyID, ev.TS, ev.CostUSD)
	}
	select {
	case s.ch <- ev:
		return nil
	case <-ctx.Done():
		if s.pending != nil {
			s.pending.SubAt(ev.ApiKeyID, ev.TS, ev.CostUSD)
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
	s.closeOnce.Do(func() { s.mu.Lock(); s.closed = true; close(s.ch); s.mu.Unlock() })
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.forceOnce.Do(func() { close(s.force) })
		<-s.done
		return ctx.Err()
	}
}

func (s *Service) dispatch() {
	defer func() { close(s.jobs); s.workers.Wait(); close(s.done) }()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]entities.UsageEvent, 0, 256)
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		job := append([]entities.UsageEvent(nil), batch...)
		select {
		case s.jobs <- job:
			batch = batch[:0]
			return true
		case <-s.force:
			return false
		}
	}
	for {
		select {
		case ev, ok := <-s.ch:
			if !ok {
				_ = flush()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= 256 && !flush() {
				return
			}
		case <-ticker.C:
			if !flush() {
				return
			}
		case <-s.force:
			return
		}
	}
}

func (s *Service) worker() {
	defer s.workers.Done()
	for batch := range s.jobs {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := s.repo.InsertBatch(ctx, batch)
			cancel()
			if err == nil {
				if s.pending != nil {
					for _, ev := range batch {
						s.pending.SubAt(ev.ApiKeyID, ev.TS, ev.CostUSD)
					}
				}
				break
			}
			select {
			case <-s.force:
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
}

func (s *Service) SpendForKeySince(ctx context.Context, id string, since time.Time) (float64, error) {
	v, err := s.repo.SpendForKeySince(ctx, id, since.UTC())
	if err != nil {
		return 0, err
	}
	if s.pending != nil {
		v += s.pending.LoadSince(id, since)
	}
	return v, nil
}
func (s *Service) Summary(ctx context.Context, since time.Time) (*entities.UsageSummary, error) {
	return s.repo.Summary(ctx, since)
}
func (s *Service) Recent(ctx context.Context, limit int) ([]entities.RecentEvent, error) {
	return s.repo.Recent(ctx, limit)
}
func (s *Service) SummaryForTenant(ctx context.Context, id string, since time.Time) (*entities.UsageSummary, error) {
	return s.repo.SummaryForTenant(ctx, id, since)
}
func (s *Service) RecentForTenant(ctx context.Context, id string, limit int) ([]entities.RecentEvent, error) {
	return s.repo.RecentForTenant(ctx, id, limit)
}
func (s *Service) Query(ctx context.Context, query entities.UsageQuery) (*entities.UsagePage, error) {
	repo, ok := s.repo.(principalRepository)
	if !ok {
		return nil, errors.New("principal usage queries unavailable")
	}
	return repo.QueryUsage(ctx, query)
}
func (s *Service) SummaryQuery(ctx context.Context, query entities.UsageQuery) (*entities.UsageSummary, error) {
	repo, ok := s.repo.(principalRepository)
	if !ok {
		return nil, errors.New("principal usage queries unavailable")
	}
	return repo.SummaryUsage(ctx, query)
}

func (s *Service) Activity(ctx context.Context, query entities.UsageQuery, groupBy string) ([]entities.UsageActivityBucket, error) {
	repo, ok := s.repo.(entities.UsageActivityRepository)
	if !ok {
		return nil, errors.New("usage activity queries unavailable")
	}
	return repo.ActivityUsage(ctx, query, groupBy)
}

func (s *Service) Health(ctx context.Context, query entities.UsageQuery) ([]entities.UsageHealthMetric, error) {
	repo, ok := s.repo.(entities.UsageHealthRepository)
	if !ok {
		return nil, errors.New("usage health queries unavailable")
	}
	return repo.HealthUsage(ctx, query)
}

const maxConversationBytes = 1 << 20

type conversationSealer interface {
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}

type conversationEnvelope struct {
	Request  string `json:"request"`
	Response string `json:"response"`
}

func (s *Service) EnableConversationCapture(box conversationSealer) { s.conversationBox = box }

func (s *Service) CaptureConversation(request, response []byte) ([]byte, bool) {
	if s.conversationBox == nil {
		return nil, false
	}
	truncated := false
	bounded := func(value []byte) string {
		if len(value) > maxConversationBytes {
			value, truncated = value[:maxConversationBytes], true
		}
		return string(value)
	}
	requestValue, responseValue := bounded(request), bounded(response)
	if requestValue == "" && responseValue == "" {
		return nil, false
	}
	plain, err := json.Marshal(conversationEnvelope{Request: requestValue, Response: responseValue})
	if err != nil {
		return nil, false
	}
	sealed, err := s.conversationBox.Seal(plain)
	if err != nil {
		return nil, false
	}
	return sealed, truncated
}

func (s *Service) Detail(ctx context.Context, id string, visibility entities.UsageVisibility) (*entities.UsageDetail, error) {
	repo, ok := s.repo.(entities.UsageDetailRepository)
	if !ok {
		return nil, errors.New("usage detail unavailable")
	}
	detail, err := repo.UsageDetail(ctx, id, visibility)
	if err != nil {
		return nil, err
	}
	if len(detail.ConversationEncrypted) == 0 || s.conversationBox == nil {
		return detail, nil
	}
	plain, err := s.conversationBox.Open(detail.ConversationEncrypted)
	if err != nil {
		return nil, errors.New("decrypt usage detail")
	}
	var envelope conversationEnvelope
	if json.Unmarshal(plain, &envelope) != nil {
		return nil, errors.New("decode usage detail")
	}
	storedTruncated := detail.ContentTruncated
	detail.Conversation, detail.ContentTruncated = normalizeConversation(envelope.Request, envelope.Response)
	detail.ContentTruncated = storedTruncated || detail.ContentTruncated
	detail.ContentAvailable = len(detail.Conversation) > 0
	return detail, nil
}
