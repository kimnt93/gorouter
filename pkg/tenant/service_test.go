package tenant

import (
	"context"
	"errors"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type repositoryStub struct{ createdName string }

func (*repositoryStub) List(context.Context) ([]entities.Tenant, error) { return nil, nil }
func (r *repositoryStub) Create(_ context.Context, name string) (*entities.Tenant, error) {
	r.createdName = name
	return &entities.Tenant{Name: name}, nil
}
func (*repositoryStub) EnsureDefault(context.Context) error { return nil }

func TestCreateValidatesAndTrimsName(t *testing.T) {
	repo := &repositoryStub{}
	svc := NewService(repo)
	if _, err := svc.Create(context.Background(), "   "); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("error = %v", err)
	}
	if _, err := svc.Create(context.Background(), " tenant one "); err != nil {
		t.Fatal(err)
	}
	if repo.createdName != "tenant one" {
		t.Fatalf("created name = %q", repo.createdName)
	}
}
