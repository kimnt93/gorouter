package pricing

import (
	"context"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type importerFunc func(context.Context) (map[string]entities.Price, error)

func (f importerFunc) Import(ctx context.Context) (map[string]entities.Price, error) { return f(ctx) }

type priceRepo map[string]entities.Price

func (r priceRepo) SetPrice(_ context.Context, model string, p entities.Price) error {
	r[model] = p
	return nil
}

func TestSync(t *testing.T) {
	repo := priceRepo{}
	s := NewService(repo, importerFunc(func(context.Context) (map[string]entities.Price, error) {
		return map[string]entities.Price{"model": {InputPerM: 3}}, nil
	}))
	if err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo["model"].InputPerM != 3 {
		t.Fatal("imported price was not persisted")
	}
}
