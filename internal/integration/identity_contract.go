package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/identity"
)

type IdentityKeyRepository interface {
	CreateOwned(context.Context, entities.ApiKey) (*entities.ApiKey, error)
	GetBySecret(context.Context, string) (*entities.ApiKey, error)
	Rotate(context.Context, string) (*entities.ApiKey, error)
	Patch(context.Context, string, *bool, *[]string, *[]string, **float64, **int) error
	CreateUserWithInitialKey(context.Context, entities.User, entities.ApiKey, []entities.AuditEvent) error
}

type IdentityUsageRepository interface {
	entities.PrincipalUsageRepository
	entities.UsageHealthRepository
	entities.UsageDetailRepository
	InsertBatch(context.Context, []entities.UsageEvent) error
}

type IdentityBackend struct {
	Identity identity.Repository
	Keys     IdentityKeyRepository
	Usage    IdentityUsageRepository
	Audit    entities.AuditRepository
}

// RunIdentityBackendContract is the single durable-backend behavior suite.
// PostgreSQL and ClickHouse tests both call this exact function.
func RunIdentityBackendContract(t *testing.T, backend IdentityBackend) {
	t.Helper()
	ctx := context.Background()
	service := identity.NewService(backend.Identity, backend.Audit)
	master := entities.Principal{Type: entities.PrincipalMaster, Username: "master"}
	suffix := strings.ToLower(entities.NewID("contract"))

	user1, err := service.CreateUser(ctx, master, " Person-"+suffix+"@Example.COM ")
	if err != nil {
		t.Fatal(err)
	}
	if user1.Username != "person-"+suffix+"@example.com" {
		t.Fatalf("normalized username=%q", user1.Username)
	}
	if _, err := service.CreateUser(ctx, master, strings.ToUpper(user1.Username)); err == nil {
		t.Fatal("duplicate normalized username accepted")
	}
	user2, err := service.CreateUser(ctx, master, "second-"+suffix+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	provisionedAt := time.Now().UTC()
	provisionedUser := entities.User{ID: entities.NewID("usr"), Username: "provisioned-" + suffix + "@example.com", NormalizedUsername: "provisioned-" + suffix + "@example.com", Status: entities.StatusActive, CreatedAt: provisionedAt, UpdatedAt: provisionedAt}
	provisionedKey := entities.ApiKey{ID: entities.NewID("key"), Name: "initial", SecretHash: "hash-" + suffix, SecretPrefix: "nr-preview", Models: []string{"m"}, Scopes: []string{entities.ScopeChat}, QuotaPeriod: entities.QuotaPeriodNone, Enabled: true, CreatedAt: provisionedAt, OwnerType: entities.OwnerUser, OwnerUserID: provisionedUser.ID}
	provisionAudit := []entities.AuditEvent{{ID: entities.NewID("audit"), TS: provisionedAt, ActorType: entities.ActorMaster, ActorID: "master", ActorLabel: "master", Action: "user.create", TargetType: "user", TargetID: provisionedUser.ID, SafeMetadata: map[string]string{"username": provisionedUser.Username}}}
	if err := backend.Keys.CreateUserWithInitialKey(ctx, provisionedUser, provisionedKey, provisionAudit); err != nil {
		t.Fatalf("compound user/key provisioning: %v", err)
	}
	if _, err := backend.Identity.UserByID(ctx, provisionedUser.ID); err != nil {
		t.Fatalf("compound user missing: %v", err)
	}
	if _, err := backend.Keys.GetBySecret(ctx, provisionedKey.SecretHash); err != nil {
		t.Fatalf("compound key missing: %v", err)
	}
	failedUser := provisionedUser
	failedUser.ID = entities.NewID("usr")
	failedKey := provisionedKey
	failedKey.ID, failedKey.OwnerUserID, failedKey.SecretHash = entities.NewID("key"), failedUser.ID, "failed-hash-"+suffix
	if err := backend.Keys.CreateUserWithInitialKey(ctx, failedUser, failedKey, nil); err == nil {
		t.Fatal("duplicate compound provisioning succeeded")
	}
	if _, err := backend.Keys.GetBySecret(ctx, failedKey.SecretHash); err == nil {
		t.Fatal("failed compound provisioning left a key behind")
	}
	organization1, err := service.CreateOrganization(ctx, master, " Acme "+suffix)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateOrganization(ctx, master, strings.ToUpper(organization1.Name)); err == nil {
		t.Fatal("duplicate normalized organization name accepted")
	}
	organization2, err := service.CreateOrganization(ctx, master, "Other "+suffix)
	if err != nil {
		t.Fatal(err)
	}
	for _, membership := range []struct {
		organization, user, role string
	}{{organization1.ID, user1.ID, entities.MembershipAdmin}, {organization1.ID, user2.ID, entities.MembershipMember}, {organization2.ID, user1.ID, entities.MembershipAdmin}} {
		if _, err := service.AddMembership(ctx, master, membership.organization, membership.user, membership.role); err != nil {
			t.Fatal(err)
		}
	}
	if memberships, err := backend.Identity.ListMembershipsForUser(ctx, user1.ID); err != nil || len(memberships) != 2 {
		t.Fatalf("multiple memberships=%+v err=%v", memberships, err)
	}
	if err := service.ChangeMembershipRole(ctx, master, organization1.ID, user1.ID, entities.MembershipMember); err != identity.ErrLastAdmin {
		t.Fatalf("last-admin demotion=%v", err)
	}

	if err := service.ValidateUserKeyContext(ctx, user1.ID, organization1.ID); err != nil {
		t.Fatal(err)
	}
	personal, err := backend.Keys.CreateOwned(ctx, entities.ApiKey{Name: "personal", OwnerType: entities.OwnerUser, OwnerUserID: user1.ID, Models: []string{"m"}, Scopes: []string{entities.ScopeChat}, QuotaPeriod: entities.QuotaPeriodNone})
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := backend.Keys.CreateOwned(ctx, entities.ApiKey{Name: "scoped", OwnerType: entities.OwnerUser, OwnerUserID: user1.ID, ContextOrganizationID: organization1.ID, Models: []string{"m"}, Scopes: []string{entities.ScopeChat}, QuotaPeriod: entities.QuotaPeriodNone})
	if err != nil {
		t.Fatal(err)
	}
	organizationKey, err := backend.Keys.CreateOwned(ctx, entities.ApiKey{Name: "organization", OwnerType: entities.OwnerOrganization, OwnerOrganizationID: organization1.ID, ContextOrganizationID: organization1.ID, Models: []string{"m"}, Scopes: []string{entities.ScopeChat}, QuotaPeriod: entities.QuotaPeriodNone})
	if err != nil {
		t.Fatal(err)
	}
	globalSharedKey, err := backend.Keys.CreateOwned(ctx, entities.ApiKey{Name: "master shared", OwnerType: entities.OwnerOrganization, OwnerOrganizationID: organization1.ID, ContextOrganizationID: organization1.ID, Models: []string{"m"}, Scopes: []string{entities.ScopeChat}, QuotaPeriod: entities.QuotaPeriodNone})
	if err != nil {
		t.Fatal(err)
	}
	loadedGlobalSharedKey, err := backend.Keys.GetBySecret(ctx, globalSharedKey.SecretHash)
	if err != nil || loadedGlobalSharedKey.CredentialOwnerUserID != "" {
		t.Fatalf("global sharing owner=%q err=%v", loadedGlobalSharedKey.CredentialOwnerUserID, err)
	}
	if personal.ContextOrganizationID != "" || scoped.ContextOrganizationID != organization1.ID || organizationKey.OwnerOrganizationID != organization1.ID {
		t.Fatal("key ownership/context was not preserved")
	}
	oldHash := personal.SecretHash
	rotated, err := backend.Keys.Rotate(ctx, personal.ID)
	if err != nil || rotated.Plaintext == "" || rotated.SecretHash == oldHash {
		t.Fatalf("rotation=%+v err=%v", rotated, err)
	}
	if _, err := backend.Keys.GetBySecret(ctx, oldHash); err == nil {
		t.Fatal("old key secret remains valid after rotation")
	}
	disabled := false
	if err := backend.Keys.Patch(ctx, organizationKey.ID, &disabled, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	loadedDisabled, err := backend.Keys.GetBySecret(ctx, organizationKey.SecretHash)
	if err != nil || loadedDisabled.Enabled {
		t.Fatalf("disabled key=%+v err=%v", loadedDisabled, err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	events := []entities.UsageEvent{
		{ID: entities.NewID("usage"), TS: now, ApiKeyID: personal.ID, CredentialID: "cred-openai", Provider: "openai", Model: "personal", Priced: true, StatusCode: 200, ActorType: entities.ActorUser, UserID: user1.ID, Username: user1.Username},
		{ID: entities.NewID("usage"), TS: now, ApiKeyID: scoped.ID, TenantID: organization1.ID, Model: "scoped", Priced: true, StatusCode: 200, ActorType: entities.ActorUser, UserID: user1.ID, Username: user1.Username, OrganizationID: organization1.ID},
		{ID: entities.NewID("usage"), TS: now, ApiKeyID: organizationKey.ID, TenantID: organization1.ID, Model: "shared", Priced: true, StatusCode: 200, ActorType: entities.ActorOrganization, Username: "org:" + organization1.Name, OrganizationID: organization1.ID},
		{ID: entities.NewID("usage"), TS: now, Model: "master", Priced: true, StatusCode: 200, ActorType: entities.ActorMaster, Username: "master"},
	}
	if err := backend.Usage.InsertBatch(ctx, events); err != nil {
		t.Fatal(err)
	}
	detail, err := backend.Usage.UsageDetail(ctx, events[0].ID, entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: user1.ID})
	if err != nil || detail.ID != events[0].ID {
		t.Fatalf("owned usage detail=%+v err=%v", detail, err)
	}
	if _, err := backend.Usage.UsageDetail(ctx, events[0].ID, entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: user2.ID}); err == nil {
		t.Fatal("foreign usage detail was visible")
	}
	userPage, err := backend.Usage.QueryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: user1.ID}, Limit: 20})
	if err != nil || len(userPage.Data) != 2 {
		t.Fatalf("user visibility=%+v err=%v", userPage, err)
	}
	organizationPage, err := backend.Usage.QueryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalOrganization, OrganizationID: organization1.ID, OrganizationWide: true}, Limit: 20})
	if err != nil || len(organizationPage.Data) != 2 {
		t.Fatalf("organization visibility=%+v err=%v", organizationPage, err)
	}
	health, err := backend.Usage.HealthUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}})
	if err != nil {
		t.Fatal(err)
	}
	foundProvider := false
	for _, metric := range health {
		if metric.Dimension == "provider" && metric.ID == "openai" && metric.Requests >= 1 && metric.Successes == metric.Requests && metric.SuccessRate == 1 {
			foundProvider = true
		}
	}
	if !foundProvider {
		t.Fatalf("provider health=%+v", health)
	}

	masterPage, err := backend.Usage.QueryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, Limit: 2})
	if err != nil || len(masterPage.Data) != 2 || masterPage.NextCursor == "" {
		t.Fatalf("master first page=%+v err=%v", masterPage, err)
	}
	secondPage, err := backend.Usage.QueryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, Cursor: masterPage.NextCursor, Limit: 2})
	if err != nil || len(secondPage.Data) == 0 {
		t.Fatalf("master second page=%+v err=%v", secondPage, err)
	}
	for _, first := range masterPage.Data {
		for _, second := range secondPage.Data {
			if first.ID == second.ID {
				t.Fatalf("cursor duplicated event %s", first.ID)
			}
		}
	}
	legacy := entities.UsageEvent{ID: entities.NewID("usage"), TS: now.Add(-time.Second), TenantID: organization1.ID, Model: "legacy", Priced: true, StatusCode: 200, ActorType: entities.ActorLegacy, Username: "legacy", OrganizationID: organization1.ID}
	if err := backend.Usage.InsertBatch(ctx, []entities.UsageEvent{legacy}); err != nil {
		t.Fatal(err)
	}
	legacyPage, err := backend.Usage.QueryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, OrganizationID: organization1.ID, Model: "legacy", Limit: 20})
	if err != nil || len(legacyPage.Data) != 1 || legacyPage.Data[0].ActorType != entities.ActorLegacy || legacyPage.Data[0].Username != "legacy" || legacyPage.Data[0].UserID != "" {
		t.Fatalf("legacy attribution=%+v err=%v", legacyPage, err)
	}
	narrowed, err := backend.Usage.QueryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: user1.ID}, OrganizationID: organization2.ID, Limit: 20})
	if err != nil || len(narrowed.Data) != 0 {
		t.Fatalf("filter broadened visibility=%+v err=%v", narrowed, err)
	}

	auditPage, err := backend.Audit.QueryAudit(ctx, entities.AuditQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, Limit: 100})
	if err != nil || len(auditPage.Data) == 0 {
		t.Fatalf("master audit=%+v err=%v", auditPage, err)
	}
	organizationAudit, err := backend.Audit.QueryAudit(ctx, entities.AuditQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalOrganization, OrganizationID: organization1.ID, OrganizationWide: true}, Limit: 100})
	if err != nil || len(organizationAudit.Data) == 0 {
		t.Fatalf("organization audit=%+v err=%v", organizationAudit, err)
	}
	for _, event := range auditPage.Data {
		encoded := strings.ToLower(event.ActorLabel + event.Action + event.TargetID)
		for key, value := range event.SafeMetadata {
			encoded += strings.ToLower(key + value)
		}
		if strings.Contains(encoded, strings.ToLower(personal.Plaintext)) || strings.Contains(encoded, strings.ToLower(personal.SecretHash)) {
			t.Fatal("audit event contains API key secret material")
		}
	}
}
