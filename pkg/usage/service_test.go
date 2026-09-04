package usage

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/seal"
)

type testRepo struct {
	mu       sync.Mutex
	failures int
	events   []entities.UsageEvent
	since    time.Time
}

func (r *testRepo) InsertBatch(_ context.Context, events []entities.UsageEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures > 0 {
		r.failures--
		return errors.New("temporary failure")
	}
	r.events = append(r.events, events...)
	return nil
}
func (r *testRepo) SpendForKeySince(_ context.Context, _ string, since time.Time) (float64, error) {
	r.since = since
	return 3, nil
}
func (*testRepo) Summary(context.Context, time.Time) (*entities.UsageSummary, error) { return nil, nil }
func (*testRepo) Recent(context.Context, int) ([]entities.RecentEvent, error)        { return nil, nil }
func (*testRepo) SummaryForTenant(context.Context, string, time.Time) (*entities.UsageSummary, error) {
	return nil, nil
}

func (r *testRepo) UsageDetail(_ context.Context, id string, _ entities.UsageVisibility) (*entities.UsageDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.events {
		if event.ID == id {
			return &entities.UsageDetail{RecentEvent: entities.RecentEvent{ID: event.ID}, ConversationEncrypted: event.ConversationEnc, ContentTruncated: event.ContentTruncated}, nil
		}
	}
	return nil, entities.ErrNotFound
}

func TestSpendForKeySinceIncludesPendingAndPassesUTCWindow(t *testing.T) {
	repo := &testRepo{}
	pending := NewPending()
	pending.Add("key", 0.25)
	svc := NewService(repo, 1, pending)
	t.Cleanup(svc.Close)
	since := time.Date(2026, time.August, 24, 7, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	spent, err := svc.SpendForKeySince(context.Background(), "key", since)
	if err != nil || spent != 3.25 {
		t.Fatalf("spent=%v err=%v", spent, err)
	}
	if repo.since.Location() != time.UTC || !repo.since.Equal(since) {
		t.Fatalf("repository since=%v, want UTC equivalent of %v", repo.since, since)
	}
}

func TestPendingSpendRespectsWindowStart(t *testing.T) {
	p := NewPending()
	start := time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC)
	p.AddAt("key", start.Add(-time.Second), 10)
	p.AddAt("key", start.Add(time.Second), .5)
	if got := p.LoadSince("key", start); got != .5 {
		t.Fatalf("window pending=%v, want .5", got)
	}
}
func (*testRepo) RecentForTenant(context.Context, string, int) ([]entities.RecentEvent, error) {
	return nil, nil
}

func TestServiceRetriesAndTracksPending(t *testing.T) {
	repo := &testRepo{failures: 1}
	pending := NewPending()
	svc := NewService(repo, 1, pending)
	ev := entities.UsageEvent{ApiKeyID: "key", CostUSD: 0.75}
	if err := svc.RecordContext(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	spent, err := svc.SpendForKeySince(context.Background(), "key", time.Time{})
	if err != nil || spent != 3.75 {
		t.Fatalf("pending spend missing: spent=%v err=%v", spent, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && pending.Load("key") != 0 {
		time.Sleep(25 * time.Millisecond)
	}
	if got := pending.Load("key"); got != 0 {
		t.Fatalf("pending cost not cleared after durable retry: %v", got)
	}
	svc.Close()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.events) != 1 {
		t.Fatalf("want one durable event, got %d", len(repo.events))
	}
	if repo.events[0].ID == "" || repo.events[0].TS.IsZero() {
		t.Fatalf("backend did not assign usage identity/time: %+v", repo.events[0])
	}
}

type concurrencyRepo struct {
	*testRepo
	active int
	max    int
}

func (r *concurrencyRepo) InsertBatch(_ context.Context, events []entities.UsageEvent) error {
	r.mu.Lock()
	r.active++
	if r.active > r.max {
		r.max = r.active
	}
	r.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	r.mu.Lock()
	r.events = append(r.events, events...)
	r.active--
	r.mu.Unlock()
	return nil
}

func TestServiceUsesConfiguredWritersAndDrains(t *testing.T) {
	repo := &concurrencyRepo{testRepo: &testRepo{}}
	svc := NewServiceWithConcurrency(repo, 100000, 4, NewPending())
	for i := 0; i < 1024; i++ {
		if err := svc.RecordContext(context.Background(), entities.UsageEvent{ApiKeyID: "key"}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := svc.CloseContext(ctx); err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.events) != 1024 {
		t.Fatalf("durable events=%d, want 1024", len(repo.events))
	}
	if repo.max < 2 {
		t.Fatalf("max concurrent writes=%d, want at least 2", repo.max)
	}
}

func TestConversationCaptureIsDisabledByDefaultAndEncryptedWhenEnabled(t *testing.T) {
	svc := NewService(&testRepo{}, 1, nil)
	defer svc.Close()
	if body, truncated := svc.CaptureConversation([]byte(`{"secret":"prompt"}`), []byte(`{"answer":"response"}`)); len(body) != 0 || truncated {
		t.Fatalf("disabled capture body=%q truncated=%v", body, truncated)
	}
	box, err := seal.New("synthetic-conversation-key")
	if err != nil {
		t.Fatal(err)
	}
	svc.EnableConversationCapture(box)
	body, truncated := svc.CaptureConversation([]byte(`{"prompt":"hello"}`), []byte(`{"answer":"world"}`))
	if truncated || bytes.Contains(body, []byte("hello")) || bytes.Contains(body, []byte("world")) {
		t.Fatalf("capture is not encrypted or unexpectedly truncated")
	}
	plain, err := box.Open(body)
	if err != nil || !bytes.Contains(plain, []byte("hello")) || !bytes.Contains(plain, []byte("world")) {
		t.Fatalf("open capture=%q err=%v", plain, err)
	}
}

func TestConversationDetailDecryptsCapturedContent(t *testing.T) {
	repo := &testRepo{}
	svc := NewService(repo, 1, nil)
	box, err := seal.New("synthetic-conversation-key")
	if err != nil {
		t.Fatal(err)
	}
	svc.EnableConversationCapture(box)
	sealed, truncated := svc.CaptureConversation([]byte(`{"prompt":"hello"}`), []byte(`{"answer":"world"}`))
	if err := svc.RecordContext(context.Background(), entities.UsageEvent{ID: "usage-detail", ConversationEnc: sealed, ContentTruncated: truncated}); err != nil {
		t.Fatal(err)
	}
	svc.Close()
	detail, err := svc.Detail(context.Background(), "usage-detail", entities.UsageVisibility{PrincipalType: entities.PrincipalMaster})
	if err != nil || !detail.ContentAvailable || len(detail.Conversation) != 2 || detail.Conversation[0].Content != `{"prompt":"hello"}` || detail.Conversation[1].Content != `{"answer":"world"}` {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
}

func TestConversationDetailNormalizesReasoningAndToolCalls(t *testing.T) {
	repo := &testRepo{}
	svc := NewService(repo, 1, nil)
	box, _ := seal.New("synthetic-conversation-key")
	svc.EnableConversationCapture(box)
	request := []byte(`{"messages":[{"role":"system","content":"instructions"},{"role":"user","content":"question"}]}`)
	response := []byte("{\"choices\":[{\"delta\":{\"reasoning_content\":\"considering\"}}]}\n{\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n{\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"arguments\":\"1}\"}}]}}]}\n{\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}")
	sealed, truncated := svc.CaptureConversation(request, response)
	if err := svc.RecordContext(context.Background(), entities.UsageEvent{ID: "usage-trace", ConversationEnc: sealed, ContentTruncated: truncated}); err != nil {
		t.Fatal(err)
	}
	svc.Close()
	detail, err := svc.Detail(context.Background(), "usage-trace", entities.UsageVisibility{PrincipalType: entities.PrincipalMaster})
	if err != nil {
		t.Fatal(err)
	}
	var reasoning, tool, answer bool
	for _, entry := range detail.Conversation {
		reasoning = reasoning || entry.Type == "reasoning" && entry.Content == "considering"
		tool = tool || entry.Type == "tool_call" && entry.Name == "lookup" && entry.Content == `{"q":1}`
		answer = answer || entry.Type == "text" && entry.Content == "answer"
	}
	if !reasoning || !tool || !answer {
		t.Fatalf("conversation=%+v", detail.Conversation)
	}
}

func BenchmarkNormalizeConversationLargeStream(b *testing.B) {
	request := `{"messages":[{"role":"user","content":"question"}]}`
	line := `{"choices":[{"delta":{"content":"abcdefghijklmnopqrstuvwxyz0123456789"}}]}` + "\n"
	response := strings.Repeat(line, 12000)
	b.ReportAllocs()
	b.SetBytes(int64(len(request) + len(response)))
	for b.Loop() {
		normalizeConversation(request, response)
	}
}
