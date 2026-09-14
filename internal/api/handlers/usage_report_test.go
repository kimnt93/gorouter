package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/internal/repositories/local"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/usage"
)

func TestUsageReportHTTPScopes(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/report.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := local.NewUsageRepo(local.New(db.DB))
	svc := usage.NewService(repo, 16, nil)
	defer svc.Close()
	now := time.Now().UTC()
	for i, e := range []entities.UsageEvent{{UserID: "u", OrganizationID: "", AgentID: "a"}, {UserID: "u", OrganizationID: "org", AgentID: "b"}, {UserID: "v", OrganizationID: "org", AgentID: "a"}} {
		e.ID = string(rune('a' + i))
		e.TS = now
		e.ActorType = entities.ActorUser
		e.CostUSD = 1
		if err = repo.InsertBatch(ctx, []entities.UsageEvent{e}); err != nil {
			t.Fatal(err)
		}
	}
	user := entities.Session{PrincipalType: entities.PrincipalUser, UserID: "u", Scopes: []string{entities.ScopeUsageRead}}
	admin := user
	admin.OrganizationID = "org"
	admin.MembershipRole = entities.MembershipAdmin
	for _, tc := range []struct {
		name, query string
		sess        entities.Session
		status      int
		count       int64
	}{
		{"personal_default", "", user, 200, 1}, {"all_owned", "scope=all_owned", user, 200, 2}, {"foreign_filter", "user_id=v", user, 200, 0}, {"multi_agent", "scope=all_owned&agent_id=a&agent_id=b", user, 200, 2}, {"org_default", "", admin, 200, 2}, {"org_no_personal", "scope=personal", admin, 403, 0}, {"forged_org", "organization_id=foreign", user, 403, 0}, {"global_denied", "scope=global", user, 403, 0}, {"no_scope", "", entities.Session{PrincipalType: entities.PrincipalUser, UserID: "u"}, 403, 0}, {"bad_dimension", "group_by=credential", user, 400, 0}, {"bad_bounds", "since=no", user, 400, 0}, {"bad_range", "range=all", user, 400, 0}, {"too_many", "agent_id=" + strings.Repeat("a,", 100) + "b", user, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Admin{UsageSvc: svc}
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error { c.Locals(localSession, &tc.sess); return a.UsageReport(c) })
			r, err := app.Test(httptest.NewRequest("GET", "/?"+tc.query, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Body.Close()
			if r.StatusCode != tc.status {
				t.Fatalf("status %d want %d", r.StatusCode, tc.status)
			}
			if tc.status == 200 {
				var report entities.UsageReport
				if err = json.NewDecoder(r.Body).Decode(&report); err != nil {
					t.Fatal(err)
				}
				if report.Totals.Requests != tc.count || report.Coverage.State != "unknown" {
					t.Fatalf("report=%+v", report)
				}
				if r.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("cacheable report")
				}
			}
		})
	}
}

func TestCanonicalMetadataPrivateAndSecretFree(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/keys.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := local.NewApiKeyRepo(local.New(db.DB))
	key, err := repo.CreatePrimary(ctx, entities.ApiKey{Name: "personal", OwnerType: entities.OwnerUser, OwnerUserID: "u", Scopes: []string{entities.ScopeChat}})
	if err != nil {
		t.Fatal(err)
	}
	a := &Admin{KeysSvc: apikey.NewService(repo, nil, nil)}
	for _, tc := range []struct {
		name   string
		sess   entities.Session
		status int
	}{
		{"owner", entities.Session{PrincipalType: entities.PrincipalUser, UserID: "u", Scopes: []string{entities.ScopeKeysManage}}, 200},
		{"foreign_admin", entities.Session{PrincipalType: entities.PrincipalUser, UserID: "other", MembershipRole: entities.MembershipAdmin, Scopes: []string{entities.ScopeKeysManage}}, 404},
		{"no_scope", entities.Session{PrincipalType: entities.PrincipalUser, UserID: "u"}, 403},
		{"master", entities.Session{PrincipalType: entities.PrincipalMaster, Role: entities.RoleMaster}, 200},
		{"view_as_master", entities.Session{PrincipalType: entities.PrincipalMaster, Role: entities.RoleMaster, OrganizationID: "org"}, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/:id", func(c fiber.Ctx) error { c.Locals(localSession, &tc.sess); return a.CanonicalUserKey(c) })
			r, err := app.Test(httptest.NewRequest("GET", "/u", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Body.Close()
			raw, _ := io.ReadAll(r.Body)
			if r.StatusCode != tc.status {
				t.Fatalf("status=%d", r.StatusCode)
			}
			for _, secret := range []string{key.Plaintext, key.SecretHash, key.SecretPrefix} {
				if secret != "" && strings.Contains(string(raw), secret) {
					t.Fatal("secret in metadata")
				}
			}
		})
	}
}

func TestCapabilitiesDoNotAdvertiseUnimplementedAccounting(t *testing.T) {
	app := fiber.New()
	a := &Admin{DatabaseBackend: "clickhouse"}
	app.Get("/", a.Capabilities)
	response, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var caps CapabilitiesResponse
	if err = json.NewDecoder(response.Body).Decode(&caps); err != nil {
		t.Fatal(err)
	}
	if caps.Backend != "clickhouse" || caps.Capabilities.DurableAcceptance || caps.Capabilities.UsageReceipts || caps.Capabilities.AtomicCreditCounters || caps.Capabilities.MemberAllocations || caps.Measurement.Coverage != "stored_only" {
		t.Fatalf("overclaimed capabilities=%+v", caps)
	}
}
