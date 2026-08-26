package llm

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestCopilotTokenExpiryAcceptsUnixAndRFC3339(t *testing.T) {
	want := time.Now().Add(time.Hour).Truncate(time.Second)
	if got := copilotTokenExpiry(strconv.FormatInt(want.Unix(), 10)); !got.Equal(want) {
		t.Fatalf("unix expiry = %v, want %v", got, want)
	}
	if got := copilotTokenExpiry(want.Format(time.RFC3339)); !got.Equal(want) {
		t.Fatalf("RFC3339 expiry = %v, want %v", got, want)
	}
}

func TestDefaultHTTPClientForcesHTTP2ForCursorDuplexStreams(t *testing.T) {
	transport, ok := NewHTTPClient().Transport.(*http.Transport)
	if !ok || !transport.ForceAttemptHTTP2 {
		t.Fatalf("transport = %#v", transport)
	}
}
