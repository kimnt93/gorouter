# Coding conventions

## Package boundaries

- Domain entities must not import Fiber, pgx, ClickHouse, Redis, or provider
  clients.
- Feature services express business behavior through consumer-owned interfaces.
- Handlers own transport parsing and presentation, not business invariants.
- Repositories own query/schema knowledge, not HTTP status decisions.
- Platform adapters translate external protocols; do not make the gateway
  switch on every provider-specific field.
- The composition root constructs concrete dependencies. Avoid hidden global
  service locators.

## Types and syntax

- Prefer concrete typed structs for JSON, SQL scan targets, domain values, and
  provider payloads. Use pointers only for meaningful optionality.
- Copy slices/maps when returning catalog or mutable state snapshots if callers
  must not mutate shared storage.
- Normalize user input once at the business boundary and persist both display
  and normalized forms where the domain requires uniqueness.
- Keep exported names/doc comments focused on public behavior; avoid comments
  that merely restate syntax.
- Use sentinel/domain errors for decisions callers must branch on. Use `%w` to
  retain causal errors internally.
- Compare secret material in constant time and store only hashes/ciphertext.
- Use UTC throughout storage and API timestamps.

## Canonical Go examples

### Consumer-owned service interface

Keep transport and database types out of the use case. Normalize and validate
at the business boundary, return domain errors callers can classify, and pass
the caller's context through unchanged.

```go
package widget

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

var ErrNameRequired = errors.New("widget name is required")

type Repository interface {
	Create(context.Context, entities.Widget) error
	ByID(context.Context, string) (*entities.Widget, error)
}

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Create(ctx context.Context, name string) (*entities.Widget, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	now := s.now()
	widget := &entities.Widget{
		ID:        entities.NewID("widget"),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, *widget); err != nil {
		return nil, err
	}
	return widget, nil
}
```

In production code, declare all required imports; the example separates the
standard library from repository imports as `gofmt` expects. Keep constructors
explicit and inject clocks only when deterministic time matters in tests.

### Operational error wrapping

Wrap at the layer that adds useful context and preserve the cause with `%w`:

```go
if err := s.repo.Create(ctx, widget); err != nil {
	return nil, fmt.Errorf("create widget: %w", err)
}
```

Branch only on stable domain/sentinel errors:

```go
if errors.Is(err, entities.ErrConflict) {
	// Map to the caller's domain or transport contract.
}
```

Do not compare error strings, create `context.Background()` inside the call,
or expose the wrapped operational message at an HTTP boundary.

### Focused service test

```go
type fakeRepository struct{}

func (*fakeRepository) Create(context.Context, entities.Widget) error { return nil }
func (*fakeRepository) ByID(context.Context, string) (*entities.Widget, error) {
	return nil, entities.ErrNotFound
}

func TestCreateRequiresName(t *testing.T) {
	service := NewService(&fakeRepository{})
	if _, err := service.Create(context.Background(), "   "); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("error=%v want %v", err, ErrNameRequired)
	}
}
```

Use table tests when several inputs exercise the same behavior. Prefer a small
typed fake over a broad mock framework when the interface is local and narrow.

## Concurrency and lifecycle

- Protect mutable process-local hints with a mutex or atomic snapshot.
- Do not hold a mutex across network/database calls.
- Respect request cancellation in provider, service, and repository calls.
- Bound goroutines, queues, request bodies, response bodies, timeouts, and
  streaming buffers.
- Drain or settle usage/quota work during errors and shutdown where possible.
- Treat queueing as a durability buffer, not as the authoritative quota total.

## Change quality

- Keep behavior changes and their tests in the same patch.
- Avoid compatibility aliases in new domain code unless a migration contract
  requires them.
- Preserve typed, fail-closed behavior instead of adding permissive fallbacks.
- Do not edit `.env`, generated Swagger, or generated SPA files by hand.
- Run `gofmt`, focused tests, full tests, vet, frontend tests/build when
  relevant, and `git diff --check`.
