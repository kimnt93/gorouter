package providerquota

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type grokTransport func(*http.Request) (*http.Response, error)

func (f grokTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGrokCredits(t *testing.T) {
	nested := []byte{0x0d, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(nested[1:], math.Float32bits(.75))
	credits := append([]byte{0x0a, byte(len(nested))}, nested...)
	frame := append([]byte{0, 0, 0, 0, byte(len(credits))}, credits...)
	frame = append(frame, []byte{0x80, 0, 0, 0, 0}...)
	client := &http.Client{Transport: grokTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "grok.com" || r.Header.Get("Authorization") != "Bearer synthetic" || r.Header.Get("Content-Type") != "application/grpc-web+proto" {
			t.Error("incorrect request")
		}
		b, _ := io.ReadAll(r.Body)
		if len(b) != 5 {
			t.Error("missing gRPC request frame")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(&byteReader{b: frame})}, nil
	})}
	window, err := New(client, nil).grokCredits(context.Background(), &entities.CredentialRuntime{OAuthAccess: "synthetic"})
	if err != nil || window.UsedPercent != 75 || window.RemainingPercent != 25 {
		t.Fatalf("window=%+v err=%v", window, err)
	}
}

type byteReader struct{ b []byte }

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}
func TestGrokInvalidQuotaNeverExhausts(t *testing.T) {
	s := New(&http.Client{Transport: grokTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(&byteReader{b: []byte{1, 2, 3}})}, nil
	})}, nil)
	if _, err := s.grokCredits(context.Background(), &entities.CredentialRuntime{OAuthAccess: "synthetic"}); err == nil {
		t.Fatal("accepted malformed quota")
	}
}
