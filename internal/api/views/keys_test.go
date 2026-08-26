package views

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestKeysTemplateRendersImportedModelsAsCheckboxes(t *testing.T) {
	data := struct {
		Title                  string
		Session                *entities.Session
		CanUsage               bool
		CanAudit               bool
		CanKeys                bool
		CanCredentials         bool
		CanModels              bool
		CanCache               bool
		CreatedSecret          string
		Keys                   []entities.ApiKey
		Tenants                []entities.Tenant
		Models                 []entities.ModelDef
		Users                  []entities.User
		Organizations          []entities.Organization
		PrincipalLabel         string
		CurrentPath            string
		ContextLocked          bool
		AllowPersonalContext   bool
		ActiveOrganizationID   string
		ActiveOrganizationName string
		KeyViews               []struct {
			Key       entities.ApiKey
			CanManage bool
		}
		CanCreateOrgKey bool
	}{
		Title:   "API keys",
		Session: &entities.Session{Role: entities.RoleMaster},
		Models: []entities.ModelDef{
			{Name: "cx/gpt-5.5", UpstreamModel: "gpt-5.5"},
			{Name: "groq/llama-3.1-8b-instant", UpstreamModel: "llama-3.1-8b-instant"},
		},
	}
	var output bytes.Buffer
	if err := Render(&output, "keys.html", data); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, marker := range []string{
		`type="checkbox" name="models" value="cx/gpt-5.5"`,
		`type="checkbox" name="models" value="groq/llama-3.1-8b-instant"`,
		`value="week">Weekly`,
	} {
		if !strings.Contains(html, marker) {
			t.Errorf("keys template missing %q", marker)
		}
	}
}

func TestKeysTemplatePromptsForProviderImportWhenNoModelsExist(t *testing.T) {
	data := struct {
		Title                  string
		Session                *entities.Session
		CanUsage               bool
		CanAudit               bool
		CanKeys                bool
		CanCredentials         bool
		CanModels              bool
		CanCache               bool
		CreatedSecret          string
		Keys                   []entities.ApiKey
		Tenants                []entities.Tenant
		Models                 []entities.ModelDef
		Users                  []entities.User
		Organizations          []entities.Organization
		PrincipalLabel         string
		CurrentPath            string
		ContextLocked          bool
		AllowPersonalContext   bool
		ActiveOrganizationID   string
		ActiveOrganizationName string
		KeyViews               []struct {
			Key       entities.ApiKey
			CanManage bool
		}
		CanCreateOrgKey bool
	}{Title: "API keys", Session: &entities.Session{Role: entities.RoleMaster}}
	var output bytes.Buffer
	if err := Render(&output, "keys.html", data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Import models from Providers first") {
		t.Fatal("missing provider-import empty state")
	}
}
