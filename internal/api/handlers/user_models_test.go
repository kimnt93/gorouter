package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/internal/repositories/local"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/orgmodel"
	"github.com/kimnt93/gorouter/pkg/seal"
	"github.com/kimnt93/gorouter/pkg/usage"
)

func TestUserKeyPersonalAndOrganizationGroups(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				ctx := context.Background()
				db, err := database.ConnectSQLite(ctx, t.TempDir()+"/router.db")
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if err = db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				store := local.New(db.DB)
				idRepo := local.NewIdentityRepo(store)
				keyRepo := local.NewApiKeyRepo(store)
				now := time.Now().UTC()
				for _, id := range []string{"admin", "user", "foreign"} {
					if err = idRepo.CreateUser(ctx, entities.User{ID: id, Username: id + "@example.test", NormalizedUsername: id + "@example.test", Status: entities.StatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
						t.Fatal(err)
					}
				}
				if err = idRepo.CreateOrganization(ctx, entities.Organization{ID: "org", Name: "xno", NormalizedName: "xno", Status: entities.StatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
				for _, id := range []string{"admin", "user"} {
					role := entities.MembershipMember
					if id == "admin" {
						role = entities.MembershipAdmin
					}
					if err = idRepo.PutMembership(ctx, entities.Membership{OrganizationID: "org", UserID: id, Role: role, CreatedAt: now}); err != nil {
						t.Fatal(err)
					}
				}
				box, _ := seal.New("synthetic-org-test")
				creds := credential.NewService(local.NewCredentialRepo(store), box)
				models := modelroute.NewService(local.NewModelRouteRepo(store))
				for _, v := range []struct{ user, model string }{{"admin", "cx/org-source"}, {"user", "cx/personal"}, {"foreign", "cx/foreign"}} {
					c, err := creds.Create(ctx, entities.CredentialInput{Name: v.model, OwnerUserID: v.user, Kind: entities.KindAPIKey, Provider: entities.ProviderOpenAICompatible, BaseURL: "https://example.test/v1", APIKey: "synthetic"})
					if err != nil {
						t.Fatal(err)
					}
					if err = models.Upsert(ctx, entities.ModelDef{Name: v.model, UpstreamModel: v.model, Enabled: true, Routes: []entities.ModelRoute{{CredentialID: c.ID, Weight: 1, Enabled: true}}}); err != nil {
						t.Fatal(err)
					}
				}
				svc := &orgmodel.Service{Repo: local.NewOrganizationModelRepo(store), Identity: idRepo, Models: models, Credentials: creds}
				admin := entities.Principal{Type: entities.PrincipalUser, UserID: "admin", Scopes: []string{entities.ScopeModelsManage}}
				limit := .000012
				alias, err := svc.Publish(ctx, admin, "org", "lite", "alias", []string{"cx/org-source"}, nil, true)
				if err != nil {
					t.Fatal(err)
				}
				group, err := svc.Publish(ctx, admin, "org", "default", "group", []string{alias.Name}, nil, true)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = svc.AssignPackage(ctx, admin, "org", group.Name, "user", &limit, true); err != nil {
					t.Fatal(err)
				}
				keys := apikey.NewService(keyRepo, local.HashSecret, local.GenerateSecret)
				key, err := keys.Create(ctx, apikey.CreateInput{Name: "one user key", OwnerType: entities.OwnerUser, OwnerUserID: "user", Scopes: []string{entities.ScopeChat, entities.ScopeUsageRead}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = keys.Create(ctx, apikey.CreateInput{Name: "second", OwnerType: entities.OwnerUser, OwnerUserID: "user"}); err != entities.ErrConflict {
					t.Fatalf("duplicate key err=%v", err)
				}
				ledger := local.NewUsageRepo(store)
				usages := usage.NewService(ledger, 16, nil)
				defer usages.Close()
				upstream := &trackingUpstream{}
				gw := &Gateway{Keys: keys, Creds: creds, Models: models, Usage: usages, OpenAI: upstream, Selector: &chat.Selector{}, Health: chat.NewHealth(), OrgModels: svc, Pricing: gatewayPriceResolver{price: entities.Price{InputPerM: 1, OutputPerM: 1}}}
				app := fiber.New()
				session := &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, PrincipalType: entities.PrincipalUser, UserID: "user", Scopes: key.Scopes}
				app.Use(func(c fiber.Ctx) error { c.Locals(localSession, session); return c.Next() })
				app.Get("/models", gw.ListModels)
				app.Post("/chat", func(c fiber.Ctx) error {
					switch protocol {
					case "responses":
						return gw.Responses(c)
					case "messages":
						return gw.Messages(c)
					default:
						return gw.Chat(c)
					}
				})
				personalActor := entities.Principal{Type: entities.PrincipalUser, UserID: "user", Scopes: []string{entities.ScopeModelsManage, entities.ScopeChat}}
				personalAlias, err := svc.PublishPersonal(ctx, personalActor, "my-model", "cx/personal", true)
				if err != nil {
					t.Fatal(err)
				}
				res, err := app.Test(httptest.NewRequest("GET", "/models", nil))
				if err != nil {
					t.Fatal(err)
				}
				var catalog llm.ModelList
				_ = json.NewDecoder(res.Body).Decode(&catalog)
				res.Body.Close()
				ids := map[string]bool{}
				for _, m := range catalog.Data {
					ids[m.ID] = true
				}
				if !ids[personalAlias.Name] || ids["cx/personal"] || !ids[alias.Name] || ids["cx/foreign"] || ids[group.Name] || ids["cx/org-source"] {
					t.Fatalf("catalog=%v", ids)
				}
				send := func(model, agent string) int {

					body := `{"model":"` + model + `","messages":[{"role":"user","content":"x"}],"max_tokens":1`
					if protocol == "responses" {
						body = `{"model":"` + model + `","input":"x","max_output_tokens":1`
					}
					if stream {
						body += `,"stream":true`
					}
					body += "}"
					r := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
					r.Header.Set("Content-Type", "application/json")
					r.Header.Set(headerAgentID, agent)
					res, err := app.Test(r)
					if err != nil {
						t.Fatal(err)
					}
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
					return res.StatusCode
				}
				if status := send(alias.Name, "agent-a"); status != 200 {
					t.Fatalf("first group=%d", status)
				}
				if status := send(alias.Name, "agent-b"); status != 429 {
					t.Fatalf("agent switched to bypass budget: %d", status)
				}
				if status := send("cx/personal", "agent-b"); status != 200 {
					t.Fatalf("personal=%d", status)
				}
				if status := send(group.Name, "agent-a"); status != 404 {
					t.Fatalf("unassigned alias=%d", status)
				}
				page, err := ledger.QueryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "user"}})
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Data) != 2 {
					t.Fatalf("usage=%d", len(page.Data))
				}
				for _, e := range page.Data {
					if e.Model == "cx/personal" && e.OrganizationID != "" {
						t.Fatal("personal leaked into org")
					}
					if e.Model == alias.Name && (e.OrganizationID != "org" || e.UserID != "user" || e.AgentID != "agent-a") {
						t.Fatal("org attribution invalid")
					}
				}
				if status := send(personalAlias.Name, "agent-personal"); status != 200 {
					t.Fatalf("personal alias=%d", status)
				}
				zero := 0.0
				if _, err = svc.Assign(ctx, admin, "org", alias.Name, "user", &zero, true); err != nil {
					t.Fatal(err)
				}
				if status := send(alias.Name, "agent-unlimited"); status != 200 {
					t.Fatalf("zero must mean unlimited=%d", status)
				}
				selfLimit := .000001
				if err = svc.SetSelfLimit(ctx, personalActor, alias.Name, &selfLimit); err != nil {
					t.Fatal(err)
				}
				if status := send(alias.Name, "agent-self"); status != 429 {
					t.Fatalf("self limit=%d", status)
				}
				if err = svc.SetSelfLimit(ctx, personalActor, alias.Name, &zero); err != nil {
					t.Fatal(err)
				}
				// Setting self limit to unlimited must not remove the org's lower cap.
				if _, err = svc.Assign(ctx, admin, "org", alias.Name, "user", &limit, true); err != nil {
					t.Fatal(err)
				}
				if status := send(alias.Name, "agent-self-zero"); status != 429 {
					t.Fatalf("org cap bypassed=%d", status)
				}
				// A personal owner can grant an alias; recipient cannot call its raw source.
				if _, err = svc.AssignPersonal(ctx, personalActor, personalAlias.Name, "foreign", &zero, true); err != nil {
					t.Fatal(err)
				}
				shared, err := svc.Available(ctx, "foreign")
				if err != nil || len(shared) != 1 || shared[0].Name != personalAlias.Name || !shared[0].Granted {
					t.Fatalf("personal share=%+v err=%v", shared, err)
				}
				foreign := entities.Principal{Type: entities.PrincipalUser, UserID: "foreign", Scopes: []string{entities.ScopeModelsManage, entities.ScopeChat}}
				if _, err = svc.PublishPersonal(ctx, foreign, "stolen", "cx/personal", true); err == nil {
					t.Fatal("recipient republished private source")
				}
				if _, err = svc.AssignPersonal(ctx, foreign, personalAlias.Name, "admin", &zero, true); err == nil {
					t.Fatal("recipient regranted private alias")
				}
				// Revocation takes effect without rotating or creating a new user key.
				if _, err = svc.Assign(ctx, admin, "org", alias.Name, "user", nil, false); err != nil {
					t.Fatal(err)
				}
				if status := send(group.Name, "agent-a"); status != 404 {
					t.Fatalf("revocation=%d", status)
				}
			})
		}
	}
}

func TestOrganizationModelAPIPermissions(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/permissions.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store := local.New(db.DB)
	identity := local.NewIdentityRepo(store)
	now := time.Now().UTC()
	if err = identity.CreateOrganization(ctx, entities.Organization{ID: "org", Name: "xno", NormalizedName: "xno", Status: entities.StatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err = identity.CreateUser(ctx, entities.User{ID: "admin", Username: "admin@example.test", NormalizedUsername: "admin@example.test", Status: entities.StatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err = identity.PutMembership(ctx, entities.Membership{OrganizationID: "org", UserID: "admin", Role: entities.MembershipAdmin, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	admin := &Admin{OrgModels: &orgmodel.Service{Repo: local.NewOrganizationModelRepo(store), Identity: identity}}
	for _, tc := range []struct {
		role, org, user string
		scopes          []string
		want            int
	}{{"master", "", "", nil, 200}, {"apikey", "", "admin", []string{entities.ScopeModelsManage}, 200}, {"apikey", "", "admin", nil, 403}, {"apikey", "", "member", []string{entities.ScopeModelsManage}, 403}, {"apikey", "other", "admin", []string{entities.ScopeModelsManage}, 403}} {
		app := fiber.New()
		app.Get("/org/:id/models", func(c fiber.Ctx) error {
			kind := entities.PrincipalUser
			if tc.role == "master" {
				kind = entities.PrincipalMaster
			}
			c.Locals(localSession, &entities.Session{Role: tc.role, PrincipalType: kind, UserID: tc.user, OrganizationID: tc.org, Scopes: tc.scopes})
			return admin.OrganizationModels(c)
		})
		res, err := app.Test(httptest.NewRequest("GET", "/org/org/models", nil))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("user=%s status=%d want=%d", tc.user, res.StatusCode, tc.want)
		}
	}
}
