package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateCheckRequiresAuthentication(t *testing.T) {
	app := New(Dependencies{})
	for _, path := range []string{"/dashboard/update", "/admin/updates/check"} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("%s returned 200 without session", path)
		}
	}
}
