package routes

import (
	"bytes"
	"io"
	"mime"
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

func TestBrandingFaviconsArePublicAndRevalidated(t *testing.T) {
	app := New(Dependencies{})
	for _, test := range []struct {
		name        string
		contentType string
	}{
		{name: "favicon.svg", contentType: "image/svg+xml"},
		{name: "favicon.ico", contentType: "image/x-icon"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := "/app-assets/assets/" + test.name
			response, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("favicon status=%d", response.StatusCode)
			}
			mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
			if err != nil || mediaType != test.contentType {
				t.Fatalf("content type=%q, want %q (parse error: %v)", response.Header.Get("Content-Type"), test.contentType, err)
			}
			if got := response.Header.Get("Cache-Control"); got != "no-cache" {
				t.Fatalf("stable-name favicon must revalidate, cache=%q", got)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			want, err := spa.Asset("assets/" + test.name)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, want) || len(body) == 0 {
				t.Fatal("favicon response differs from the embedded artwork")
			}
		})
	}
}

func TestLoginAndDashboardShareBrandingFavicons(t *testing.T) {
	index, err := spa.Index()
	if err != nil {
		t.Fatal(err)
	}
	app := New(Dependencies{})
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/login", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d", response.StatusCode)
	}
	login, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"dashboard": index, "login": login} {
		for _, icon := range []string{"favicon.svg", "favicon.ico"} {
			link := regexp.MustCompile(`<link\b[^>]*rel="icon"[^>]*href="/app-assets/assets/` + regexp.QuoteMeta(icon) + `"`)
			if !link.Match(body) {
				t.Errorf("%s does not reference the shared %s", name, icon)
			}
		}
	}
}
