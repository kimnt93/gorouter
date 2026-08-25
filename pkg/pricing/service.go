package pricing

import (
	"context"
	"errors"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type Repository interface {
	SetPrice(ctx context.Context, model string, price entities.Price) error
}

// Importer is an alias at the use-case boundary for OpenRouter/LiteLLM-backed
// implementations.
type Importer = entities.PriceImporter

type Service struct {
	repo     Repository
	importer Importer
}

func NewService(repo Repository, importer Importer) *Service {
	return &Service{repo: repo, importer: importer}
}

func (s *Service) Sync(ctx context.Context) error {
	if s.repo == nil || s.importer == nil {
		return errors.New("price sync is not configured")
	}
	prices, err := s.importer.Import(ctx)
	if err != nil {
		return err
	}
	for model, price := range prices {
		if err := s.repo.SetPrice(ctx, model, price); err != nil {
			return err
		}
	}
	return nil
}

// Start runs synchronization independently of request serving. Errors are
// reported without stopping future attempts.
func (s *Service) Start(ctx context.Context, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := s.Sync(ctx); err != nil && report != nil {
				report(err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
