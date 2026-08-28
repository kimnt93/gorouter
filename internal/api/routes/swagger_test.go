package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimnt93/gorouter/internal/docs"
)

func TestSwaggerDocumentsEveryJSONRoute(t *testing.T) {
	var specification struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(docs.SwaggerInfo.ReadDoc()), &specification); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"/healthz": {"get"}, "/login": {"post"}, "/logout": {"post"}, "/admin/session": {"get"}, "/v1/chat/completions": {"post"}, "/v1/responses": {"post"}, "/v1/messages": {"post"}, "/v1/models": {"get"},
		"/admin/tenants": {"get", "post"}, "/admin/organizations": {"get", "post"}, "/admin/organizations/{id}": {"get", "patch"}, "/admin/organizations/{id}/members": {"get", "post"}, "/admin/organizations/{id}/members/{user_id}": {"patch", "delete"}, "/admin/users": {"get", "post"}, "/admin/users/{id}": {"get", "patch"}, "/admin/audit/events": {"get"},
		"/admin/credentials": {"get", "post"}, "/admin/credentials/{id}": {"put", "delete"}, "/admin/providers": {"get"}, "/admin/oauth/{provider}/start": {"post"}, "/admin/oauth/{provider}/complete": {"post"}, "/admin/credentials/{id}/test": {"post"}, "/admin/credentials/{id}/quota": {"get", "post"}, "/admin/credentials/{id}/reset-credits": {"get", "post"}, "/admin/credentials/{id}/models": {"get"}, "/admin/credentials/{id}/models/import": {"post"}, "/admin/credentials/{id}/models/refresh": {"post"}, "/admin/credentials/{id}/chat-tests": {"post"},
		"/admin/api-keys": {"get", "post"}, "/admin/api-keys/models": {"get"}, "/admin/api-keys/{id}": {"patch", "delete"}, "/admin/api-keys/{id}/rotate": {"post"}, "/admin/models": {"get"}, "/admin/models/{name}": {"put", "delete"}, "/admin/prices": {"get"}, "/admin/prices/{model}": {"put", "delete"}, "/admin/pricing/catalog": {"get"}, "/admin/pricing/estimate": {"get"}, "/admin/usage/summary": {"get"}, "/admin/usage/recent": {"get"}, "/admin/usage/events/{id}": {"get"}, "/admin/usage/activity": {"get"}, "/admin/cache/stats": {"get"}, "/admin/cache/flush": {"post"},
	}
	for path, methods := range want {
		operations, ok := specification.Paths[path]
		if !ok {
			t.Errorf("Swagger missing path %s", path)
			continue
		}
		for _, method := range methods {
			if _, ok := operations[method]; !ok {
				t.Errorf("Swagger missing %s %s", method, path)
			}
		}
	}
	for path, operations := range specification.Paths {
		if _, ok := want[path]; !ok {
			t.Errorf("Swagger contains unregistered path %s", path)
		}
		for method := range operations {
			found := false
			for _, candidate := range want[path] {
				if method == candidate {
					found = true
				}
			}
			if !found {
				t.Errorf("Swagger contains unregistered %s %s", method, path)
			}
		}
	}
}

func TestSwaggerUIIsServed(t *testing.T) {
	app := New(Dependencies{})
	request := httptest.NewRequest(http.MethodGet, "/docs", nil)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /docs=%d", response.StatusCode)
	}
}
