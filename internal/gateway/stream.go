package gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/internal/cache"
	"github.com/kimnt93/gorouter/internal/llm"
)

func (s *Server) serveCached(w http.ResponseWriter, cc *chatContext, e *cache.Entry) {
	w.Header().Set("X-Cache", "hit")
	ev := s.event(cc, "", cc.Model.UpstreamModel, llm.Usage{PromptTokens: e.PromptTok, CompletionTokens: e.Completion}, time.Since(cc.Start).Milliseconds(), 200, "")
	if cc.Parsed.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		var resp llm.Response
		if json.Unmarshal(e.Body, &resp) == nil && len(resp.Choices) > 0 {
			content, _ := resp.Choices[0].Message.Content.(string)
			first := llm.Chunk{
				ID: resp.ID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: resp.Model,
				Choices: []llm.ChunkChoice{{Index: 0, Delta: llm.Delta{Role: "assistant", Content: content}}},
			}
			b, _ := json.Marshal(first)
			_ = llm.WriteSSE(w, flusher, "", b)
			end := llm.Chunk{
				ID: resp.ID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: resp.Model,
				Choices: []llm.ChunkChoice{{Index: 0, FinishReason: resp.Choices[0].FinishReason}},
				Usage:   &resp.Usage,
			}
			b2, _ := json.Marshal(end)
			_ = llm.WriteSSE(w, flusher, "", b2)
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	} else {
		ct := e.ContentType
		if ct == "" {
			ct = "application/json"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Upstream-Credential", "cache")
		_, _ = w.Write(e.Body)
	}
	s.Usage.Submit(ev)
}

func (s *Server) nonStreamUpstream(w http.ResponseWriter, cc *chatContext, rt *llm.CredentialRuntime, upstreamModel string, res *llm.Result) {
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	duration := time.Since(cc.Start).Milliseconds()
	if err != nil {
		writeErr(w, 502, "upstream read error", "upstream_error", "")
		return
	}
	if res.StatusCode >= 400 {
		w.Header().Set("X-Cache", "bypass")
		w.Header().Set("X-Upstream-Credential", rt.ID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.StatusCode)
		_, _ = w.Write(bodyBytes)
		return
	}
	respBody := bodyBytes
	usage := llm.Usage{}
	cacheable := cache.Deterministic(cc.Parsed) && s.cfg.Cache.Enabled
	if rt.Provider == llm.ProviderAnthropic {
		resp, terr := llm.FromAnthropic(bodyBytes, cc.Parsed.Model)
		if terr != nil {
			writeErr(w, 502, "upstream translation failed: "+terr.Error(), "upstream_error", "")
			return
		}
		respBody, _ = json.Marshal(resp)
		usage = resp.Usage
	} else {
		var parsed llm.Response
		if json.Unmarshal(bodyBytes, &parsed) == nil && parsed.Usage.TotalTokens() > 0 {
			usage = parsed.Usage
		} else {
			usage.PromptTokens = cc.Parsed.EstimatePromptTokens()
			usage.CompletionTokens = 16
			cacheable = false
		}
	}
	ev := s.event(cc, rt.ID, upstreamModel, usage, duration, res.StatusCode, "")
	s.Usage.Submit(ev)
	s.addPending(cc.Key.ID, ev.CostUSD)
	header := "off"
	if cacheable {
		header = "miss"
		s.Cache.Store(cc.Key.ID, cc.Key.TenantID, cc.Model.Name, cc.RawBody, &cache.Entry{
			Status: 200, ContentType: "application/json", Body: respBody,
			PromptTok: usage.PromptTokens, Completion: usage.CompletionTokens,
		})
	}
	w.Header().Set("X-Cache", header)
	w.Header().Set("X-Upstream-Credential", rt.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
}

func (s *Server) streamUpstream(w http.ResponseWriter, cc *chatContext, rt *llm.CredentialRuntime, upstreamModel string, res *llm.Result) {
	flusher, canFlush := w.(http.Flusher)
	defer res.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Upstream-Credential", rt.ID)
	w.WriteHeader(http.StatusOK)
	if canFlush {
		flusher.Flush()
	}

	var (
		usage      llm.Usage
		contentBuf strings.Builder
		clientGone bool
	)

	if rt.Provider == llm.ProviderAnthropic {
		conv := llm.NewAnthropicStreamConverter(cc.Parsed.Model)
		err := llm.ScanSSE(res.Body, func(e llm.SSEEvent) error {
			chunks, _, ferr := conv.Feed(e.Event, e.Data)
			if ferr != nil {
				return ferr
			}
			for _, cb := range chunks {
				if clientGone {
					return nil
				}
				if werr := llm.WriteSSE(w, flusher, "", cb); werr != nil {
					clientGone = true
					return nil
				}
			}
			return nil
		})
		if err != nil && !clientGone {
			log.Printf("anthropic stream error: %v", err)
		}
		usage = conv.UsageCollected()
	} else {
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if _, werr := io.WriteString(w, line+"\n"); werr != nil {
				clientGone = true
				break
			}
			if line == "" {
				if canFlush {
					flusher.Flush()
				}
				continue
			}
			payload, ok := strings.CutPrefix(line, "data: ")
			if !ok || payload == "[DONE]" {
				continue
			}
			var probe struct {
				Usage   *llm.Usage `json:"usage"`
				Choices []struct {
					Delta        llm.Delta `json:"delta"`
					FinishReason string    `json:"finish_reason"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(payload), &probe) == nil {
				if probe.Usage != nil && probe.Usage.TotalTokens() > 0 {
					usage = *probe.Usage
				}
				for _, ch := range probe.Choices {
					contentBuf.WriteString(ch.Delta.Content)
				}
			}
		}
		if canFlush {
			flusher.Flush()
		}
	}

	duration := time.Since(cc.Start).Milliseconds()
	if usage.PromptTokens == 0 {
		usage.PromptTokens = cc.Parsed.EstimatePromptTokens()
	}
	if usage.CompletionTokens == 0 {
		usage.CompletionTokens = llm.EstimateTextTokens(contentBuf.String())
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = 16
		}
	}
	status := 200
	errLabel := ""
	if clientGone {
		status = 499
		errLabel = "client disconnected"
	}
	ev := s.event(cc, rt.ID, upstreamModel, usage, duration, status, errLabel)
	s.Usage.Submit(ev)
	s.addPending(cc.Key.ID, ev.CostUSD)

	if !clientGone && cache.Deterministic(cc.Parsed) && contentBuf.Len() > 0 && s.cfg.Cache.Enabled {
		full := llm.Response{
			ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   cc.Parsed.Model,
			Choices: []llm.Choice{{
				Index:        0,
				Message:      &llm.ResponseMessage{Role: "assistant", Content: contentBuf.String()},
				FinishReason: "stop",
			}},
			Usage: usage,
		}
		body, _ := json.Marshal(full)
		s.Cache.Store(cc.Key.ID, cc.Key.TenantID, cc.Model.Name, cc.RawBody, &cache.Entry{
			Status: 200, ContentType: "application/json", Body: body, Stream: true,
			PromptTok: usage.PromptTokens, Completion: usage.CompletionTokens,
		})
	}
}
