package routes

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/kimnt93/gorouter/internal/api/spa"
)

func TestSPAAssetAndAuthenticationBoundary(t *testing.T) {
	index, err := spa.Index()
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`/app-assets/assets/[^"']+\.js`).Find(index)
	if len(match) == 0 {
		t.Fatal("built SPA JavaScript asset not found")
	}
	app := New(Dependencies{})
	asset, err := app.Test(httptest.NewRequest(http.MethodGet, string(match), nil))
	if err != nil {
		t.Fatal(err)
	}
	defer asset.Body.Close()
	if asset.StatusCode != http.StatusOK || asset.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset status=%d cache=%q", asset.StatusCode, asset.Header.Get("Cache-Control"))
	}
	page, err := app.Test(httptest.NewRequest(http.MethodGet, "/dashboard/logs", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	if page.StatusCode != http.StatusSeeOther || page.Header.Get("Location") != "/login" {
		t.Fatalf("unauthenticated dashboard status=%d location=%q", page.StatusCode, page.Header.Get("Location"))
	}
}
