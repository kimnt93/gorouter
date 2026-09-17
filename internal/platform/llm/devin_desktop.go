package llm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// Devin Desktop (formerly Windsurf) uses an imported key, not OAuth. The
// metadata key authenticates a short-lived JWT preflight; the JWT is never saved.
type DevinDesktopAdapter struct{ HTTP *http.Client }

func (a *DevinDesktopAdapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}
func devinMeta(key, session, jwt string) []byte {
	parts := [][]byte{protoString(1, "windsurf"), protoString(2, "1.48.2"), protoString(3, key), protoString(4, "en-US"), protoString(7, "3.6.27"), protoString(10, session), protoString(12, "windsurf")}
	if jwt != "" {
		parts = append(parts, protoString(21, jwt))
	}
	return bytes.Join(parts, nil)
}
func devinHeaders(request *http.Request, connect bool) {
	request.Header.Set("Connect-Protocol-Version", "1")
	if connect {
		request.Header.Set("Content-Type", "application/connect+proto")
		request.Header.Set("Accept", "application/connect+proto")
		request.Header.Set("Connect-Accept-Encoding", "gzip")
		request.Header.Set("User-Agent", "windsurf/3.6.27")
	} else {
		request.Header.Set("Content-Type", "application/proto")
		request.Header.Set("Accept", "*/*")
	}
}
func (a *DevinDesktopAdapter) authenticate(ctx context.Context, base, key, session string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/exa.auth_pb.AuthService/GetUserJwt", bytes.NewReader(protoBytes(1, devinMeta(key, session, ""))))
	if err != nil {
		return "", 0, err
	}
	devinHeaders(req, false)
	resp, err := a.client().Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", resp.StatusCode, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, err
	}
	jwt := string(protoField(raw, 1))
	if jwt == "" {
		return "", 0, errors.New("Devin authentication response omitted JWT")
	}
	return jwt, 200, nil
}
func devinBase(base string) string {
	if base == "" {
		return "https://server.codeium.com"
	}
	return strings.TrimRight(base, "/")
}
func devinRequest(input ChatRequest, model, key, session, jwt, cascade string) []byte {
	parts := [][]byte{protoBytes(1, devinMeta(key, session, jwt))}
	var system []string
	for _, msg := range input.Messages {
		text := rawText(msg.Content)
		if msg.Role == "system" || msg.Role == "developer" {
			system = append(system, text)
			continue
		}
		id, _ := randomUUID()
		source := uint64(1)
		if msg.Role == "assistant" {
			source = 2
		} else if msg.Role == "tool" {
			source = 4
		}
		prompt := [][]byte{protoString(1, id), append(protoTag(2, 0), protoVarint(source)...), protoString(3, text)}
		for _, call := range msg.ToolCalls {
			if call.ID != "" {
				prompt = append(prompt, protoBytes(6, bytes.Join([][]byte{protoString(1, call.ID), protoString(2, call.Function.Name), protoString(3, call.Function.Arguments)}, nil)))
			}
		}
		if msg.ToolCallID != "" {
			prompt = append(prompt, protoString(7, msg.ToolCallID))
		}
		parts = append(parts, protoBytes(3, bytes.Join(prompt, nil)))
	}
	if len(system) > 0 {
		parts = append(parts, protoString(2, strings.Join(system, "\n\n")))
	}
	parts = append(parts, append(protoTag(7, 0), 5))
	for _, tool := range input.Tools {
		if tool.Type != "function" || tool.Function.Name == "" {
			continue
		}
		schema := tool.Function.Parameters
		if len(schema) == 0 {
			schema = []byte("{}")
		}
		parts = append(parts, protoBytes(10, bytes.Join([][]byte{protoString(1, tool.Function.Name), protoString(2, tool.Function.Description), protoString(3, string(schema))}, nil)))
	}
	if choice := strings.Trim(string(input.ToolChoice), "\""); choice == "auto" || choice == "none" || choice == "required" {
		parts = append(parts, protoBytes(12, protoString(1, choice)))
	} else if len(input.ToolChoice) > 0 && input.ToolChoice[0] == '{' {
		var selected struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if json.Unmarshal(input.ToolChoice, &selected) == nil && selected.Type == "function" && selected.Function.Name != "" {
			parts = append(parts, protoBytes(12, protoString(2, selected.Function.Name)))
		}
	}
	parts = append(parts, protoString(14, model), protoString(16, cascade), protoString(21, model))
	return cursorConnectFrame(bytes.Join(parts, nil))
}
func (a *DevinDesktopAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	key := cr.APIKey
	if key == "" {
		return nil, errors.New("Devin Desktop key unavailable")
	}
	var input ChatRequest
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	session, _ := randomUUID()
	cascade := input.ConversationID
	if cascade == "" {
		cascade, _ = randomUUID()
	}
	base := devinBase(cr.BaseURL)
	jwt, status, err := a.authenticate(ctx, base, key, session)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return &entities.UpstreamResult{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Devin Desktop authentication failed"}}`))}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/exa.api_server_pb.ApiServerService/GetChatMessage", bytes.NewReader(devinRequest(input, model, key, session, jwt, cascade)))
	if err != nil {
		return nil, err
	}
	devinHeaders(req, true)
	resp, err := a.client().Do(req)
	if err != nil {
		return nil, err
	}
	result := &entities.UpstreamResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body}
	if resp.StatusCode != 200 {
		return result, nil
	}
	if input.Stream {
		reader, writer := io.Pipe()
		go func() { defer resp.Body.Close(); writer.CloseWithError(devinStream(resp.Body, writer, model)) }()
		result.Body = reader
		result.Header = http.Header{"Content-Type": []string{"text/event-stream"}}
		return result, nil
	}
	defer resp.Body.Close()
	message := ResponseMessage{Role: "assistant"}
	var usage Usage
	err = readDevinFrames(resp.Body, func(frame []byte) error {
		message.Content += string(protoField(frame, 3))
		message.ReasoningContent += string(protoField(frame, 9))
		for _, v := range protoFields(frame, 6) {
			id := string(protoField(v, 1))
			if id != "" {
				message.ToolCalls = append(message.ToolCalls, ToolCall{ID: id, Type: "function", Function: ToolFunction{Name: string(protoField(v, 2)), Arguments: string(protoField(v, 3))}})
			}
		}
		if v := protoField(frame, 7); len(v) > 0 {
			usage = devinUsage(v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	finish := "stop"
	if len(message.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	encoded, _ := json.Marshal(Response{ID: "chatcmpl-" + session, Object: "chat.completion", Created: time.Now().Unix(), Model: model, Choices: []Choice{{Message: &message, FinishReason: finish}}, Usage: usage})
	result.Body = io.NopCloser(bytes.NewReader(encoded))
	result.Header = http.Header{"Content-Type": []string{"application/json"}}
	return result, nil
}
func devinUsage(raw []byte) Usage {
	var usage Usage
	for _, spec := range []struct {
		field int
		dest  *int64
	}{{2, &usage.PromptTokens}, {3, &usage.CompletionTokens}, {4, &usage.CacheWriteTokens}, {5, &usage.CacheReadTokens}} {
		*spec.dest = int64(protoNumber(raw, spec.field))
	}
	return usage
}
func protoNumber(data []byte, want int) uint64 {
	offset := 0
	for offset < len(data) {
		tag, ok := readProtoVarint(data, &offset)
		if !ok {
			return 0
		}
		wire := tag & 7
		switch wire {
		case 0:
			v, ok := readProtoVarint(data, &offset)
			if !ok {
				return 0
			}
			if int(tag>>3) == want {
				return v
			}
		case 2:
			n, ok := readProtoVarint(data, &offset)
			if !ok || n > uint64(len(data)-offset) {
				return 0
			}
			offset += int(n)
		default:
			return 0
		}
	}
	return 0
}
func readDevinFrames(reader io.Reader, visit func([]byte) error) error {
	for {
		var header [5]byte
		_, err := io.ReadFull(reader, header[:])
		if err == io.EOF {
			return errors.New("Devin stream missing terminal frame")
		}
		if err != nil {
			return err
		}
		size := binary.BigEndian.Uint32(header[1:])
		if size > 16<<20 {
			return errors.New("Devin frame too large")
		}
		frame := make([]byte, int(size))
		if _, err = io.ReadFull(reader, frame); err != nil {
			return err
		}
		if header[0]&^byte(3) != 0 {
			return errors.New("invalid Devin frame")
		}
		if header[0]&1 != 0 {
			gz, e := gzip.NewReader(bytes.NewReader(frame))
			if e != nil {
				return e
			}
			frame, e = io.ReadAll(io.LimitReader(gz, (16<<20)+1))
			gz.Close()
			if e != nil {
				return e
			}
			if len(frame) > 16<<20 {
				return errors.New("Devin frame too large")
			}
		}
		if header[0]&2 != 0 {
			if bytes.Contains(frame, []byte(`"code"`)) {
				return errors.New("Devin stream ended with upstream error")
			}
			return nil
		}
		if err := visit(frame); err != nil {
			return err
		}
	}
}
func devinStream(upstream io.Reader, out io.Writer, model string) error {
	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	emit := func(delta Delta, finish string) error {
		chunk, _ := json.Marshal(Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Delta: delta, FinishReason: finish}}})
		_, err := fmt.Fprintf(out, "data: %s\n\n", chunk)
		return err
	}
	err := readDevinFrames(upstream, func(frame []byte) error {
		if text := string(protoField(frame, 3)); text != "" {
			if e := emit(Delta{Role: "assistant", Content: text}, ""); e != nil {
				return e
			}
		}
		if reasoning := string(protoField(frame, 9)); reasoning != "" {
			if err := emit(Delta{ReasoningContent: reasoning}, ""); err != nil {
				return err
			}
		}
		for index, tool := range protoFields(frame, 6) {
			id := string(protoField(tool, 1))
			if id == "" {
				continue
			}
			idx := index
			if err := emit(Delta{ToolCalls: []ToolCall{{Index: &idx, ID: id, Type: "function", Function: ToolFunction{Name: string(protoField(tool, 2)), Arguments: string(protoField(tool, 3))}}}}, ""); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err = emit(Delta{}, "stop"); err != nil {
		return err
	}
	_, err = io.WriteString(out, "data: [DONE]\n\n")
	return err
}
func (a *DevinDesktopAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	session, _ := randomUUID()
	_, status, err := a.authenticate(ctx, devinBase(cr.BaseURL), cr.APIKey, session)
	return status, err
}
func (a *DevinDesktopAdapter) DiscoverModels(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return modelsFor("devin-desktop", "swe-1-7", "swe-1-7-lightning"), nil
}
