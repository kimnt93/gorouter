package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestDevinDesktopChatAndAuth(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/exa.auth_pb.AuthService/GetUserJwt" {
			if !bytes.Contains(b, []byte("synthetic-key")) {
				t.Error("key not sent in metadata")
			}
			w.Write(protoString(1, "synthetic-jwt"))
			return
		}
		if !bytes.Contains(b, []byte("synthetic-jwt")) || !bytes.Contains(b, []byte("swe-1-7")) {
			t.Error("chat metadata missing")
		}
		w.Header().Set("Content-Type", "application/connect+proto")
		w.Write(cursorConnectFrame(protoString(3, "hello")))
		w.Write([]byte{2, 0, 0, 0, 0})
	}))
	defer server.Close()
	cr := &entities.CredentialRuntime{Provider: "devin-desktop", Kind: entities.KindAPIKey, APIKey: "synthetic-key", BaseURL: server.URL}
	raw := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	adapter := &DevinDesktopAdapter{HTTP: server.Client()}
	result, err := adapter.Send(context.Background(), cr, "swe-1-7", raw)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	var response Response
	if err = json.NewDecoder(result.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Choices[0].Message.Content != "hello" || len(calls) != 2 {
		t.Fatalf("response=%+v paths=%v", response, calls)
	}
	streaming := []byte(`{"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	result, err = adapter.Send(context.Background(), cr, "swe-1-7", streaming)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, _ := io.ReadAll(result.Body)
	if !strings.Contains(string(body), "hello") || !strings.Contains(string(body), "[DONE]") {
		t.Fatalf("invalid stream")
	}
}
func TestDevinDesktopAuthFailureDoesNotSendChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetChatMessage") {
			t.Error("chat sent after failed auth")
		}
		w.WriteHeader(401)
	}))
	defer server.Close()
	a := &DevinDesktopAdapter{HTTP: server.Client()}
	result, err := a.Send(context.Background(), &entities.CredentialRuntime{APIKey: "synthetic", BaseURL: server.URL}, "swe-1-7", []byte(`{"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if result.StatusCode != 401 {
		t.Fatalf("status=%d", result.StatusCode)
	}
}
