package llm

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
)

type SSEEvent struct {
	Event string
	Data  []byte
}

func ScanSSE(r io.Reader, fn func(SSEEvent) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var event string
	var data bytes.Buffer
	flush := func() error {
		if data.Len() == 0 && event == "" {
			return nil
		}
		ev := event
		if ev == "" {
			ev = "message"
		}
		payload := bytes.TrimSuffix(data.Bytes(), []byte("\n"))
		payloadCopy := append([]byte(nil), payload...)
		event = ""
		data.Reset()
		return fn(SSEEvent{Event: ev, Data: payloadCopy})
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if err := flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			data.WriteByte('\n')
		default:
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return sc.Err()
}

func WriteSSE(w http.ResponseWriter, flusher http.Flusher, event string, data []byte) error {
	if event != "" && event != "message" {
		if _, err := io.WriteString(w, "event: "+event+"\n"); err != nil {
			return err
		}
	}
	if _, err := w.Write(append(append([]byte("data: "), data...), '\n', '\n')); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}
