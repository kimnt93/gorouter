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

type viewIdentityRepository struct{}

func (*viewIdentityRepository) CreateUser(context.Context, entities.User) error { return nil }
func (*viewIdentityRepository) UserByID(_ context.Context, id string) (*entities.User, error) {
	if id != "user_view" {
		return nil, entities.ErrNotFound
	}
	return &entities.User{ID: id, Username: "view@example.com", Status: entities.StatusActive}, nil
}
func (*viewIdentityRepository) UserByNormalizedUsername(context.Context, string) (*entities.User, error) {
	return nil, entities.ErrNotFound
}
func (*viewIdentityRepository) ListUsers(context.Context, entities.PageQuery) ([]entities.User, string, error) {
	return nil, "", nil
}
func (*viewIdentityRepository) UpdateUserStatus(context.Context, string, string, time.Time) error {
	return nil
}
func (*viewIdentityRepository) CreateOrganization(context.Context, entities.Organization) error {
	return nil
}
func (*viewIdentityRepository) OrganizationByID(_ context.Context, id string) (*entities.Organization, error) {
	if id != "org_view" {
		return nil, entities.ErrNotFound
	}
	return &entities.Organization{ID: id, Name: "View Org", Status: entities.StatusActive}, nil
}
func (*viewIdentityRepository) OrganizationByNormalizedName(context.Context, string) (*entities.Organization, error) {
	return nil, entities.ErrNotFound
}
func (*viewIdentityRepository) ListOrganizations(context.Context, entities.PageQuery) ([]entities.Organization, string, error) {
	return nil, "", nil
}
func (*viewIdentityRepository) UpdateOrganization(context.Context, entities.Organization) error {
	return nil
}
func (*viewIdentityRepository) CreateMembership(context.Context, entities.Membership) error {
	return nil
}
func (*viewIdentityRepository) PutMembership(context.Context, entities.Membership) error { return nil }
func (*viewIdentityRepository) Membership(_ context.Context, organizationID, userID string) (*entities.Membership, error) {
	if organizationID != "org_view" || userID != "user_view" {
		return nil, entities.ErrNotFound
	}
	return &entities.Membership{OrganizationID: organizationID, UserID: userID, Role: entities.MembershipMember}, nil
}
func (*viewIdentityRepository) ListMemberships(context.Context, string) ([]entities.Membership, error) {
	return nil, nil
}
func (*viewIdentityRepository) ListMembershipsForUser(context.Context, string) ([]entities.Membership, error) {
	return nil, nil
}
func (*viewIdentityRepository) CountActiveOrganizationAdmins(context.Context, string) (int, error) {
	return 1, nil
}
func (*viewIdentityRepository) DeleteMembership(context.Context, string, string) error { return nil }

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
func (*activityRepository) QueryUsage(context.Context, entities.UsageQuery) (*entities.UsagePage, error) {
	return &entities.UsagePage{}, nil
}
func (*activityRepository) SummaryUsage(context.Context, entities.UsageQuery) (*entities.UsageSummary, error) {
	return &entities.UsageSummary{ByModel: map[string]entities.ModelU{}}, nil
}
func (*activityRepository) HealthUsage(context.Context, entities.UsageQuery) ([]entities.UsageHealthMetric, error) {
	return []entities.UsageHealthMetric{{Dimension: "provider", ID: "openai", Requests: 2, Successes: 2, SuccessRate: 1}}, nil
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
	if body.GroupBy != "week" || len(body.Data) != 1 || body.Data[0].CacheWriteTokens != 12 || len(body.Health) != 1 || body.Health[0].ID != "openai" {
		t.Fatalf("response=%+v", body)
	}
	if repository.groupBy != "week" || repository.query.UserID != "user_1" || repository.query.APIKeyID != "key_1" || repository.query.Since == nil {
		t.Fatalf("query=%+v group=%q", repository.query, repository.groupBy)
	}
}

func TestUsageActivityRejectsMultipleOrganizationContextsForUser(t *testing.T) {
	repository := &activityRepository{}
	service := usage.NewService(repository, 1, nil)
	t.Cleanup(service.Close)
	admin := &Admin{UsageSvc: service}
	app := fiber.New()
	app.Get("/activity", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, PrincipalType: entities.PrincipalUser, UserID: "user_1", Scopes: []string{entities.ScopeUsageRead}})
		return c.Next()
	}, admin.UsageActivity)
	response, err := app.Test(httptest.NewRequest("GET", "/activity?organization_id=org_1,org_2", nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status=%d, want %d", response.StatusCode, fiber.StatusBadRequest)
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

func TestUsageActivityMasterViewAsMemberUsesMemberVisibility(t *testing.T) {
	repository := &activityRepository{}
	service := usage.NewService(repository, 1, nil)
	t.Cleanup(service.Close)
	admin := &Admin{UsageSvc: service, IdentityRepo: &viewIdentityRepository{}}
	app := fiber.New()
	app.Get("/activity", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster})
		return c.Next()
	}, admin.UsageActivity)
	response, err := app.Test(httptest.NewRequest("GET", "/activity?range=7d&view_user_id=user_view&organization_id=org_view", nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	visibility := repository.query.Visibility
	if visibility.PrincipalType != entities.PrincipalUser || visibility.UserID != "user_view" || visibility.OrganizationID != "org_view" || visibility.OrganizationWide {
		t.Fatalf("visibility=%+v", visibility)
	}
}
