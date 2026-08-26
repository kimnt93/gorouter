package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kimnt93/gorouter/pkg/entities"
	"io"
	"net/http"
	"net/url"
)

type cursorDriver struct{}

func (cursorDriver) Start(_ context.Context, _ *Service, f flow) (flow, StartResult, error) {
	f.flowType = "cursor_poll"
	f.extra["uuid"] = f.state
	q := url.Values{"challenge": {pkceChallenge(f.verifier)}, "uuid": {f.state}, "mode": {"login"}, "redirectTarget": {"cli"}}
	return f, StartResult{AuthorizeURL: "https://cursor.com/loginDeepControl?" + q.Encode(), Instructions: "Open the Cursor login page and approve access. This dialog will poll securely for completion."}, nil
}
func (cursorDriver) Complete(ctx context.Context, s *Service, f flow, _ string) (tokenResponse, error) {
	q := url.Values{"uuid": {f.extra["uuid"]}, "verifier": {f.verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api2.cursor.sh/auth/poll?"+q.Encode(), nil)
	if err != nil {
		return tokenResponse{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return tokenResponse{}, ErrAuthorizationPending
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tokenResponse{}, fmt.Errorf("Cursor OAuth poll returned HTTP %d", resp.StatusCode)
	}
	var p map[string]any
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p) != nil {
		return tokenResponse{}, errors.New("invalid Cursor auth response")
	}
	t := tokenFromPayload(p)
	if t.AccessToken == "" || t.RefreshToken == "" {
		return tokenResponse{}, errors.New("Cursor auth response omitted tokens")
	}
	t.Metadata = entities.OAuthMetadata{MachineID: f.extra["uuid"]}
	return t, nil
}
