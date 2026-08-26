package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type activityRepository struct {
	query   entities.UsageQuery
	groupBy string
}

func (*activityRepository) InsertBatch(context.Context, []entities.UsageEvent) error { return nil }
func (*activityRepository) SpendForKeySince(context.Context, string, time.Time) (float64, error) {
	return 0, nil
}
func (*activityRepository) Summary(context.Context, time.Time) (*entities.UsageSummary, error) {
	return &entities.UsageSummary{}, nil
}
func (*activityRepository) Recent(context.Context, int) ([]entities.RecentEvent, error) {
	return nil, nil
}
func (*activityRepository) SummaryForTenant(context.Context, string, time.Time) (*entities.UsageSummary, error) {
	return &entities.UsageSummary{}, nil
}
func (*activityRepository) RecentForTenant(context.Context, string, int) ([]entities.RecentEvent, error) {
	return nil, nil
}
func (r *activityRepository) ActivityUsage(_ context.Context, query entities.UsageQuery, groupBy string) ([]entities.UsageActivityBucket, error) {
	r.query, r.groupBy = query, groupBy
	return []entities.UsageActivityBucket{{Start: time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC), Requests: 2, CacheWriteTokens: 12}}, nil
}

func TestUsageActivityParsesFiltersAndReturnsTypedBuckets(t *testing.T) {
	repository := &activityRepository{}
	service := usage.NewService(repository, 1, nil)
	t.Cleanup(service.Close)
	admin := &Admin{UsageSvc: service}
	app := fiber.New()
	app.Get("/activity", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster})
		return c.Next()
	}, admin.UsageActivity)
	response, err := app.Test(httptest.NewRequest("GET", "/activity?range=90d&group_by=week&user_id=user_1&api_key_id=key_1", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	var body UsageActivityResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.GroupBy != "week" || len(body.Data) != 1 || body.Data[0].CacheWriteTokens != 12 {
		t.Fatalf("response=%+v", body)
	}
	if repository.groupBy != "week" || repository.query.UserID != "user_1" || repository.query.APIKeyID != "key_1" || repository.query.Since == nil {
		t.Fatalf("query=%+v group=%q", repository.query, repository.groupBy)
	}
}

func TestUsageActivityRejectsIncompleteCustomRange(t *testing.T) {
	service := usage.NewService(&activityRepository{}, 1, nil)
	t.Cleanup(service.Close)
	admin := &Admin{UsageSvc: service}
	app := fiber.New()
	app.Get("/activity", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster})
		return c.Next()
	}, admin.UsageActivity)
	response, err := app.Test(httptest.NewRequest("GET", "/activity?range=custom&group_by=day&since=2026-08-01T00:00:00Z", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
