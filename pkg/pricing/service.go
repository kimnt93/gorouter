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

type RefreshLocker interface {
	WithLock(ctx context.Context, key string, fn func() error) (bool, error)
}

type CatalogRepository interface {
	ReplaceCatalogPrices(ctx context.Context, source string, prices []entities.CatalogPrice) error
}

// Importer is an alias at the use-case boundary for OpenRouter/LiteLLM-backed
// implementations.
type Importer = entities.PriceImporter

type Service struct {
	repo     Repository
	importer Importer
}

// CatalogService owns background synchronization. Its first fetch is immediate
// and request serving remains independent of failures on subsequent fetches.
type CatalogService struct {
	repo     CatalogRepository
	importer entities.CatalogImporter
	source   string
	resolver *Resolver
	locker   RefreshLocker
}

func NewService(repo Repository, importer Importer) *Service {
	return &Service{repo: repo, importer: importer}
}

func NewCatalogService(repo CatalogRepository, importer entities.CatalogImporter, source string, resolver *Resolver) *CatalogService {
	return &CatalogService{repo: repo, importer: importer, source: source, resolver: resolver}
}

func (s *CatalogService) SetRefreshLocker(locker RefreshLocker) { s.locker = locker }

func (s *CatalogService) Sync(ctx context.Context) error {
	if s.repo == nil || s.importer == nil {
		return errors.New("catalog price sync is not configured")
	}
	if s.locker != nil {
		_, err := s.locker.WithLock(ctx, "pricing-"+s.source, func() error { return s.syncLocked(ctx) })
		return err
	}
	return s.syncLocked(ctx)
}

func (s *CatalogService) syncLocked(ctx context.Context) error {
	prices, err := s.importer.ImportCatalog(ctx)
	if err != nil {
		return err
	}
	if len(prices) == 0 {
		return errors.New("catalog price sync returned no models")
	}
	if err := s.repo.ReplaceCatalogPrices(ctx, s.source, prices); err != nil {
		return err
	}
	if s.resolver != nil {
		if err := s.resolver.Refresh(ctx); err != nil {
			return err
		}
		s.resolver.NotifyChange(ctx)
	}
	return nil
}

func (s *CatalogService) Start(ctx context.Context, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		if err := s.Sync(ctx); err != nil && report != nil {
			report(err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Sync(ctx); err != nil && report != nil {
					report(err)
				}
			}
		}
	}()
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
