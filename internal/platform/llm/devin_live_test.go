package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/internal/platform/modeldiscovery"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// Opt-in diagnostic: exactly two sequential text turns. Never print response
// content, raw CLI errors or credentials; output only timings and counts.
func TestDevinLiveTimings(t *testing.T) {
	if os.Getenv("TEST_DEVIN_LIVE") != "1" {
		t.Skip("live provider tests are opt-in")
	}
	binary := os.Getenv("TEST_DEVIN_CLI_BINARY")
	if binary == "" {
		t.Skip("CLI binary unset")
	}
	key := os.Getenv("DEVIN_TEST_KEY")
	if key == "" {
		t.Skip("key unset")
	}
	a := &DevinCLIAdapter{Binary: binary, WorkDir: t.TempDir(), CatalogCache: modeldiscovery.NewMemorySnapshots(8)}
	cr := &entities.CredentialRuntime{ID: "bounded-live-test", Provider: "devin-cli", APIKey: key}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	start := time.Now()
	m, err := a.RefreshModels(ctx, cr)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("catalog cold_ms=%.2f families=%d", float64(time.Since(start).Microseconds())/1000, len(m))
	for i := 0; i < 5; i++ {
		start = time.Now()
		m, err = a.DiscoverModels(ctx, cr)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("catalog warm_sample=%d ms=%.3f families=%d", i+1, float64(time.Since(start).Microseconds())/1000, len(m))
	}
	// Two sequential tiny text requests, same model/effort and prompt. No response
	// text is retained. This is a diagnostic sample, not a throughput benchmark.
	for _, cold := range []bool{true, false} {
		if cold {
			a.invalidateCatalog(ctx, cr)
		}
		start = time.Now()
		r, err := a.Send(ctx, cr, "swe-2", []byte(`{"stream":true,"messages":[{"role":"user","content":"Reply with exactly: OK"}],"reasoning":{"effort":"medium"}}`))
		if err != nil {
			t.Fatal(err)
		}
		headers := time.Since(start)
		if r.StatusCode != 200 {
			r.Body.Close()
			t.Fatalf("status=%d", r.StatusCode)
		}
		first := time.Duration(0)
		done := false
		reader := bufio.NewReader(r.Body)
		for {
			line, e := reader.ReadString('\n')
			if e != nil {
				if e != io.EOF {
					t.Fatal(e)
				}
				break
			}
			if strings.Contains(line, "[DONE]") {
				done = true
			}
			if strings.HasPrefix(line, "data: {") {
				var c devinChunk
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &c)
				for _, v := range c.Choices {
					if v.Delta.Content != "" && first == 0 {
						first = time.Since(start)
					}
				}
			}
		}
		r.Body.Close()
		if !done || first == 0 {
			t.Fatal("incomplete stream")
		}
		t.Logf("chat cold=%v status=200 headers_ms=%d text_ttft_ms=%d total_ms=%d", cold, headers.Milliseconds(), first.Milliseconds(), time.Since(start).Milliseconds())
	}
}
