package integration

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
)

func atomicAdd(p *int64) int64 { return atomic.AddInt64(p, 1) }

func httpNewRequest(method, url string, body io.Reader) (*http.Request, error) {
	return http.NewRequest(method, url, body)
}

func httpDo(req *http.Request) (*http.Response, error) {
	return http.DefaultClient.Do(req)
}

func bytesReader(s string) io.Reader { return strings.NewReader(s) }

func httptestNewServer(h http.HandlerFunc) *httptest.Server { return httptest.NewServer(h) }

func contextBackground() context.Context { return context.Background() }

func testCtx() context.Context { return ctxBg }

func containsStr(b []byte, s string) bool {
	return strings.Contains(string(b), s)
}
