package handlers

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/internal/api/views"
	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestIdentityPagesRenderDesktopAndMobileSafeMarkup(t *testing.T) {
	now := time.Now().UTC()
	organization := entities.Organization{ID: "org-1", Name: "Acme", Status: entities.StatusActive, CreatedAt: now}
	user := entities.User{ID: "usr-1", Username: "person@example.com", Status: entities.StatusActive, CreatedAt: now}
	key := entities.ApiKey{ID: "key-1", Name: "login", SecretPrefix: "nr-preview", OwnerType: entities.OwnerUser, OwnerUserID: user.ID, Enabled: true, CreatedAt: now}
	membership := entities.Membership{OrganizationID: organization.ID, UserID: user.ID, Role: entities.MembershipAdmin}
	data := pageData{
		Title: "Smoke", Session: &entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster}, PrincipalLabel: "master", CurrentPath: "/ui/usage",
		CanUsage: true, CanAudit: true, CanKeys: true, CanCredentials: true, CanModels: true, CanCache: true,
		User: &user, Organization: &organization, Users: []entities.User{user}, Organizations: []entities.Organization{organization}, Memberships: []entities.Membership{membership}, Keys: []entities.ApiKey{key},
		KeyViews: []keyView{{Key: key, CanManage: true}}, UserViews: []userView{{User: user, OrganizationCount: 1, KeyCount: 1}}, OrganizationViews: []organizationView{{Organization: organization, MemberCount: 1, AdminCount: 1, KeyCount: 1}}, MembershipViews: []membershipView{{Membership: membership, Username: user.Username, OrganizationName: organization.Name}},
		Summary: &entities.UsageSummary{}, Recent: []entities.RecentEvent{{ID: "usage-1", TS: now, Model: "model-a", Username: user.Username, ActorType: entities.ActorUser, StatusCode: 200}}, AuditEvents: []entities.AuditEvent{{ID: "audit-1", TS: now, ActorLabel: "master", Action: "user.create", TargetType: "user", TargetID: user.ID, SafeMetadata: map[string]string{"status": "active"}}}, Filters: map[string]string{}, CanManageMembers: true,
	}
	for _, templateName := range []string{"users.html", "user.html", "organizations.html", "organization.html", "keys.html", "usage.html", "audit.html"} {
		t.Run(templateName, func(t *testing.T) {
			var output bytes.Buffer
			if err := views.Render(&output, templateName, data); err != nil {
				t.Fatal(err)
			}
			html := output.String()
			if !strings.Contains(html, `<meta name="viewport"`) || !strings.Contains(html, "Log out") {
				t.Fatalf("missing responsive shell in %s", templateName)
			}
			if strings.Contains(html, "SecretHash") || strings.Contains(strings.ToLower(html), "authorization header") {
				t.Fatalf("unsafe material rendered in %s", templateName)
			}
		})
	}
	css, err := views.ReadAsset("app.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"@media(max-width:720px)", ".responsive-table", ".console-dialog", "max-height:min(82vh"} {
		if !strings.Contains(string(css), marker) {
			t.Errorf("responsive CSS missing %q", marker)
		}
	}
}

func TestOneTimeSecretAppearsOnlyInsideAcknowledgementDialog(t *testing.T) {
	secret := "nr-one-time-secret-do-not-persist"
	data := pageData{Title: "Keys", Session: &entities.Session{Role: entities.RoleMaster}, PrincipalLabel: "master", CurrentPath: "/ui/keys", CreatedSecret: secret, Filters: map[string]string{}}
	var output bytes.Buffer
	if err := views.Render(&output, "keys.html", data); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if strings.Count(html, secret) != 1 || !strings.Contains(html, "I copied it") || !strings.Contains(html, "<dialog") {
		t.Fatalf("one-time secret dialog contract failed")
	}
}
