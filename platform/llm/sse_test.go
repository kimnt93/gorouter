package llm

import (
	"strings"
	"testing"
)

func TestScanSSEAcceptsCRLFAndPreservesEvents(t *testing.T) {
	var events []SSEEvent
	err := ScanSSE(strings.NewReader("event: message\r\ndata: {\"ok\":true}\r\n\r\ndata: [DONE]\r\n\r\n"), func(event SSEEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Event != "message" || string(events[0].Data) != `{"ok":true}` || string(events[1].Data) != "[DONE]" {
		t.Fatalf("events = %+v", events)
	}
}
