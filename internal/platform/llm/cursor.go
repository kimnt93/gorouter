package llm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type CursorAdapter struct {
	HTTP      *http.Client
	Persister OAuthTokenPersister
}

func (a *CursorAdapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}

func (a *CursorAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	var input ChatRequest
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	conversation := StableConversationID(&input)
	if conversation == "" {
		conversation, _ = randomUUID()
	}
	message, _ := randomUUID()
	payload := cursorAgentRequest(input, model, conversation, message)
	frame := cursorConnectFrame(payload)
	endpoint, err := a.agentEndpoint(ctx, cr)
	if err != nil {
		return nil, err
	}
	requestReader, requestWriter := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, requestReader)
	if err != nil {
		requestReader.Close()
		requestWriter.Close()
		return nil, err
	}
	req.ContentLength = -1
	requestID, _ := randomUUID()
	trace := strings.ReplaceAll(requestID, "-", "") + strings.Repeat("0", 32)
	trace = trace[:32]
	span := strings.ReplaceAll(message, "-", "")
	span = (span + strings.Repeat("0", 16))[:16]
	req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(cr.OAuthAccess, "cursor-session-token:"))
	req.Header.Set("Content-Type", "application/connect+proto")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Connect-Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "connect-es/1.6.1")
	req.Header.Set("X-Cursor-Client-Type", "cli")
	req.Header.Set("X-Cursor-Client-Version", "2026.08.20")
	req.Header.Set("X-Ghost-Mode", "true")
	req.Header.Set("X-Request-Id", requestID)
	req.Header.Set("Traceparent", "00-"+trace+"-"+span+"-01")
	writeErr := make(chan error, 1)
	go func() {
		_, err := requestWriter.Write(frame)
		writeErr <- err
	}()
	resp, err := a.client().Do(req)
	if err != nil {
		requestWriter.CloseWithError(err)
		requestReader.Close()
		return nil, err
	}
	select {
	case err := <-writeErr:
		if err != nil {
			resp.Body.Close()
			requestWriter.CloseWithError(err)
			return nil, err
		}
	default:
	}
	if resp.StatusCode == http.StatusUnauthorized && a.Persister != nil && canRetryOAuth(ctx) {
		resp.Body.Close()
		requestWriter.Close()
		if err := a.refresh(ctx, cr); err != nil {
			return nil, err
		}
		return a.Send(markOAuthRetry(ctx), cr, model, raw)
	}
	result := &entities.UpstreamResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		requestWriter.Close()
		return result, nil
	}
	if input.Stream {
		result.Body = cursorStream(resp.Body, requestWriter, model)
		result.Header["Content-Type"] = []string{"text/event-stream"}
		return result, nil
	}
	defer resp.Body.Close()
	defer requestWriter.Close()
	var content strings.Builder
	tokens := int64(0)
	messageOut := ResponseMessage{Role: "assistant"}
	err = readCursorFrames(resp.Body, requestWriter, func(event cursorEvent) {
		if event.Kind == "text" {
			content.WriteString(event.Text)
		}
		if event.Kind == "tokens" {
			tokens += event.Tokens
		}
		if event.Kind == "tool" {
			messageOut.ToolCalls = append(messageOut.ToolCalls, event.ToolCall)
		}
	})
	if err != nil {
		return nil, err
	}
	messageOut.Content = content.String()
	finish := "stop"
	if len(messageOut.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	response := Response{ID: "chatcmpl-" + requestID, Object: "chat.completion", Created: time.Now().Unix(), Model: model, Choices: []Choice{{Index: 0, Message: &messageOut, FinishReason: finish}}, Usage: Usage{CompletionTokens: tokens}}
	encoded, _ := json.Marshal(response)
	result.Body = io.NopCloser(bytes.NewReader(encoded))
	result.Header["Content-Type"] = []string{"application/json"}
	return result, nil
}

func (a *CursorAdapter) refresh(ctx context.Context, cr *entities.CredentialRuntime) error {
	return refreshOAuthJSON(ctx, a.HTTP, a.Persister, cr, "https://api2.cursor.sh/auth/exchange_user_api_key", map[string]any{}, map[string]string{"Authorization": "Bearer " + cr.OAuthRefreh})
}

func (a *CursorAdapter) agentEndpoint(ctx context.Context, cr *entities.CredentialRuntime) (string, error) {
	base := strings.TrimRight(cr.BaseURL, "/")
	if base == "" {
		base = "https://api2.cursor.sh"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/aiserver.v1.ServerConfigService/GetServerConfig", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(cr.OAuthAccess, "cursor-session-token:"))
	req.Header.Set("Content-Type", "application/proto")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("User-Agent", "connect-es/1.6.1")
	req.Header.Set("X-Cursor-Client-Type", "cli")
	req.Header.Set("X-Cursor-Client-Version", "2026.08.20")
	resp, err := a.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Cursor server config returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	field := protoField(body, 27)
	if len(field) == 0 {
		return "", fmt.Errorf("Cursor server config omitted Agent URL")
	}
	agent := string(protoField(field, 1))
	parsed, parseErr := url.Parse(agent)
	if parseErr != nil || parsed.Scheme != "https" || parsed.User != nil || (parsed.Hostname() != "cursor.sh" && !strings.HasSuffix(parsed.Hostname(), ".cursor.sh")) {
		return "", fmt.Errorf("Cursor server config returned invalid Agent URL")
	}
	return strings.TrimRight(agent, "/") + "/agent.v1.AgentService/Run", nil
}

func cursorFlatten(messages []Message) string {
	var parts []string
	for _, m := range messages {
		value := rawText(m.Content)
		if len(m.ToolCalls) > 0 {
			calls, _ := json.Marshal(m.ToolCalls)
			value += "\nTool calls: " + string(calls)
		}
		if m.Role == "tool" {
			value = "Tool result for " + m.ToolCallID + ": " + value
		}
		if value != "" {
			parts = append(parts, strings.ToUpper(m.Role)+": "+value)
		}
	}
	return strings.Join(parts, "\n\n")
}
func protoVarint(value uint64) []byte {
	out := []byte{}
	for value >= 128 {
		out = append(out, byte(value)|0x80)
		value >>= 7
	}
	return append(out, byte(value))
}
func protoTag(field, wire int) []byte { return protoVarint(uint64(field<<3 | wire)) }
func protoBytes(field int, value []byte) []byte {
	return append(append(protoTag(field, 2), protoVarint(uint64(len(value)))...), value...)
}
func protoString(field int, value string) []byte { return protoBytes(field, []byte(value)) }
func protoBool(field int, value bool) []byte {
	n := byte(0)
	if value {
		n = 1
	}
	return append(protoTag(field, 0), n)
}
func protoDouble(field int, value float64) []byte {
	out := append([]byte{}, protoTag(field, 1)...)
	bits := make([]byte, 8)
	binary.LittleEndian.PutUint64(bits, math.Float64bits(value))
	return append(out, bits...)
}
func cursorConnectFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}
func cursorProtoValue(value any) []byte {
	switch typed := value.(type) {
	case nil:
		return append(protoTag(1, 0), 0)
	case bool:
		return protoBool(4, typed)
	case float64:
		return protoDouble(2, typed)
	case string:
		return protoString(3, typed)
	case []any:
		parts := make([][]byte, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, protoBytes(1, cursorProtoValue(item)))
		}
		return protoBytes(6, bytes.Join(parts, nil))
	case map[string]any:
		parts := make([][]byte, 0, len(typed))
		for key, item := range typed {
			entry := append(protoString(1, key), protoBytes(2, cursorProtoValue(item))...)
			parts = append(parts, protoBytes(1, entry))
		}
		return protoBytes(5, bytes.Join(parts, nil))
	default:
		return protoString(3, fmt.Sprint(value))
	}
}
func cursorToolDefinition(tool Tool) []byte {
	var schema any = map[string]any{"type": "object", "properties": map[string]any{}}
	if len(tool.Function.Parameters) > 0 {
		_ = json.Unmarshal(tool.Function.Parameters, &schema)
	}
	return bytes.Join([][]byte{
		protoString(1, tool.Function.Name),
		protoString(2, tool.Function.Description),
		protoBytes(3, cursorProtoValue(schema)),
		protoString(4, "gorouter"),
		protoString(5, tool.Function.Name),
	}, nil)
}
func cursorAgentRequest(input ChatRequest, model, conversation, message string) []byte {
	text := cursorFlatten(input.Messages)
	user := bytes.Join([][]byte{protoString(1, text), protoString(2, message), protoBytes(3, nil), append(protoTag(4, 0), 1)}, nil)
	action := protoBytes(2, protoBytes(1, protoBytes(1, user)))
	details := protoBytes(3, bytes.Join([][]byte{protoString(1, model), protoString(3, model), protoString(4, model)}, nil))
	requestedParts := [][]byte{protoString(1, model)}
	if input.Reasoning != nil && input.Reasoning.Effort != "" {
		parameter := append(protoString(1, "reasoning"), protoString(2, input.Reasoning.Effort)...)
		requestedParts = append(requestedParts, protoBytes(3, parameter))
	}
	requested := protoBytes(9, bytes.Join(requestedParts, nil))
	toolParts := make([][]byte, 0, len(input.Tools))
	if string(input.ToolChoice) != `"none"` {
		for _, tool := range input.Tools {
			toolParts = append(toolParts, protoBytes(1, cursorToolDefinition(tool)))
		}
	}
	mcpTools := protoBytes(4, bytes.Join(toolParts, nil))
	run := bytes.Join([][]byte{protoBytes(1, nil), action, details, mcpTools, protoString(5, conversation), requested, append(protoTag(12, 0), 0), protoString(16, conversation)}, nil)
	return protoBytes(1, run)
}

func readProtoVarint(data []byte, offset *int) (uint64, bool) {
	var value uint64
	for shift := 0; shift < 64 && *offset < len(data); shift += 7 {
		b := data[*offset]
		*offset++
		value |= uint64(b&0x7f) << shift
		if b < 128 {
			return value, true
		}
	}
	return 0, false
}
func protoField(data []byte, want int) []byte {
	offset := 0
	for offset < len(data) {
		tag, ok := readProtoVarint(data, &offset)
		if !ok {
			return nil
		}
		field, wire := int(tag>>3), int(tag&7)
		switch wire {
		case 0:
			_, ok = readProtoVarint(data, &offset)
			if !ok {
				return nil
			}
		case 2:
			size, valid := readProtoVarint(data, &offset)
			if !valid || size > uint64(len(data)-offset) {
				return nil
			}
			value := data[offset : offset+int(size)]
			offset += int(size)
			if field == want {
				return value
			}
		case 1:
			offset += 8
		case 5:
			offset += 4
		default:
			return nil
		}
	}
	return nil
}
func protoFields(data []byte, want int) [][]byte {
	values := [][]byte{}
	offset := 0
	for offset < len(data) {
		tag, ok := readProtoVarint(data, &offset)
		if !ok {
			break
		}
		field, wire := int(tag>>3), int(tag&7)
		if wire == 0 {
			value, _ := readProtoVarint(data, &offset)
			if field == want {
				values = append(values, protoVarint(value))
			}
			continue
		}
		if wire != 2 {
			break
		}
		size, ok := readProtoVarint(data, &offset)
		if !ok || size > uint64(len(data)-offset) {
			break
		}
		value := data[offset : offset+int(size)]
		offset += int(size)
		if field == want {
			values = append(values, value)
		}
	}
	return values
}

type cursorEvent struct {
	Kind     string
	Text     string
	Tokens   int64
	ToolCall ToolCall
}

type cursorExecEvent struct {
	ID        uint64
	ExecID    string
	Variant   int
	Payload   []byte
	ToolName  string
	Arguments string
}

func cursorExecServerEvent(payload []byte) *cursorExecEvent {
	execMessages := protoFields(payload, 2)
	if len(execMessages) == 0 {
		return nil
	}
	message := execMessages[0]
	event := &cursorExecEvent{ExecID: string(protoField(message, 15))}
	if values := protoFields(message, 1); len(values) > 0 {
		offset := 0
		event.ID, _ = readProtoVarint(values[0], &offset)
	}
	for _, field := range []int{2, 3, 4, 5, 7, 8, 9, 10, 11, 14, 16, 20, 23} {
		if value := protoField(message, field); value != nil {
			event.Variant = field
			event.Payload = value
			break
		}
	}
	if event.Variant == 11 {
		event.ToolName = string(protoField(event.Payload, 5))
		if event.ToolName == "" {
			event.ToolName = string(protoField(event.Payload, 1))
		}
		args := map[string]any{}
		for _, entry := range protoFields(event.Payload, 2) {
			key := string(protoField(entry, 1))
			value := protoField(entry, 2)
			if key != "" && value != nil {
				args[key] = cursorDecodeProtoValue(value)
			}
		}
		encoded, _ := json.Marshal(args)
		event.Arguments = string(encoded)
	}
	return event
}

func cursorDecodeProtoValue(data []byte) any {
	if value := protoField(data, 3); value != nil {
		return string(value)
	}
	if value := protoField(data, 5); value != nil {
		out := map[string]any{}
		for _, entry := range protoFields(value, 1) {
			key := string(protoField(entry, 1))
			if item := protoField(entry, 2); key != "" && item != nil {
				out[key] = cursorDecodeProtoValue(item)
			}
		}
		return out
	}
	if value := protoField(data, 6); value != nil {
		out := []any{}
		for _, item := range protoFields(value, 1) {
			out = append(out, cursorDecodeProtoValue(item))
		}
		return out
	}
	for _, field := range []int{4, 1} {
		if values := protoFields(data, field); len(values) > 0 {
			offset := 0
			value, _ := readProtoVarint(values[0], &offset)
			if field == 4 {
				return value != 0
			}
			return nil
		}
	}
	return nil
}

func cursorExecResponse(event *cursorExecEvent) []byte {
	if event == nil {
		return nil
	}
	var resultField int
	var result []byte
	switch event.Variant {
	case 10:
		resultField = 10
		result = protoBytes(1, protoBytes(1, nil))
	case 2, 3, 4, 7, 8, 16:
		resultField = map[int]int{2: 2, 3: 3, 4: 4, 7: 7, 8: 8, 16: 16}[event.Variant]
		path := string(protoField(event.Payload, 1))
		rejected := append(protoString(1, path), protoString(2, "Tool not available in this environment. Use declared MCP tools instead.")...)
		result = protoBytes(2, rejected)
	case 5:
		resultField = 5
		result = protoBytes(2, protoString(1, "Tool not available in this environment. Use declared MCP tools instead."))
	case 9:
		resultField = 9
		result = nil
	case 20, 23:
		resultField = event.Variant
		result = protoBytes(2, protoString(1, "Tool not available in this environment. Use declared MCP tools instead."))
	case 14:
		resultField = 2
		result = protoBytes(2, protoString(3, "Tool not available in this environment. Use declared MCP tools instead."))
	default:
		return nil
	}
	inner := bytes.Join([][]byte{append(protoTag(1, 0), protoVarint(event.ID)...), protoString(15, event.ExecID), protoBytes(resultField, result)}, nil)
	return cursorConnectFrame(protoBytes(2, inner))
}

func cursorKVResponse(payload []byte) []byte {
	messages := protoFields(payload, 4)
	if len(messages) == 0 {
		return nil
	}
	message := messages[0]
	var id uint64
	if values := protoFields(message, 1); len(values) > 0 {
		offset := 0
		id, _ = readProtoVarint(values[0], &offset)
	}
	parts := [][]byte{}
	if id != 0 {
		parts = append(parts, append(protoTag(1, 0), protoVarint(id)...))
	}
	if protoField(message, 2) != nil {
		parts = append(parts, protoBytes(2, protoBytes(1, nil)))
	} else if protoField(message, 3) != nil {
		parts = append(parts, protoBytes(3, nil))
	} else {
		return nil
	}
	if metadata := protoField(message, 4); len(metadata) > 0 {
		parts = append(parts, protoBytes(4, metadata))
	}
	return cursorConnectFrame(protoBytes(3, bytes.Join(parts, nil)))
}

func readCursorFrames(reader io.Reader, responder io.Writer, visit func(cursorEvent)) error {
	header := make([]byte, 5)
	for {
		if _, err := io.ReadFull(reader, header); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		size := int(binary.BigEndian.Uint32(header[1:5]))
		if size < 0 || size > 16<<20 {
			return fmt.Errorf("invalid Cursor frame")
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return err
		}
		if header[0]&1 != 0 {
			gz, err := gzip.NewReader(bytes.NewReader(payload))
			if err != nil {
				return err
			}
			payload, err = io.ReadAll(io.LimitReader(gz, 16<<20))
			gz.Close()
			if err != nil {
				return err
			}
		}
		if header[0]&2 != 0 {
			return nil
		}
		if response := cursorKVResponse(payload); len(response) > 0 && responder != nil {
			if _, err := responder.Write(response); err != nil {
				return err
			}
		}
		if execEvent := cursorExecServerEvent(payload); execEvent != nil {
			if response := cursorExecResponse(execEvent); len(response) > 0 && responder != nil {
				if _, err := responder.Write(response); err != nil {
					return err
				}
			}
			if execEvent.Variant == 11 && execEvent.ToolName != "" {
				visit(cursorEvent{Kind: "tool", ToolCall: ToolCall{
					ID:       "call_" + strings.ReplaceAll(execEvent.ExecID, "-", ""),
					Type:     "function",
					Function: ToolFunction{Name: execEvent.ToolName, Arguments: execEvent.Arguments},
				}})
				// Cursor pauses here waiting for the tool result. Finish this OpenAI
				// turn; the next request cold-resumes from the supplied tool history.
				return nil
			}
		}
		for _, interaction := range protoFields(payload, 1) {
			for _, value := range protoFields(interaction, 1) {
				visit(cursorEvent{Kind: "text", Text: string(protoField(value, 1))})
			}
			for _, value := range protoFields(interaction, 4) {
				visit(cursorEvent{Kind: "reasoning", Text: string(protoField(value, 1))})
			}
			for _, value := range protoFields(interaction, 8) {
				raw := protoField(value, 1)
				offset := 0
				n, _ := readProtoVarint(raw, &offset)
				visit(cursorEvent{Kind: "tokens", Tokens: int64(n)})
			}
			if len(protoFields(interaction, 14)) > 0 {
				return nil
			}
		}
	}
}
func cursorStream(upstream io.ReadCloser, responder io.WriteCloser, model string) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer upstream.Close()
		defer responder.Close()
		defer writer.Close()
		id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
		created := time.Now().Unix()
		finish := "stop"
		toolIndex := 0
		_ = readCursorFrames(upstream, responder, func(event cursorEvent) {
			if event.Kind != "text" && event.Kind != "reasoning" && event.Kind != "tool" {
				return
			}
			delta := Delta{Role: "assistant", Content: event.Text}
			if event.Kind == "tool" {
				idx := toolIndex
				toolIndex++
				event.ToolCall.Index = &idx
				delta.ToolCalls = []ToolCall{event.ToolCall}
				finish = "tool_calls"
			}
			chunk := Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: delta}}}
			encoded, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(writer, "data: %s\n\n", encoded)
		})
		final := Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: Delta{}, FinishReason: finish}}}
		encoded, _ := json.Marshal(final)
		_, _ = fmt.Fprintf(writer, "data: %s\n\ndata: [DONE]\n\n", encoded)
	}()
	return reader
}

func (a *CursorAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	_, err := a.agentEndpoint(ctx, cr)
	if err != nil {
		return 0, err
	}
	return http.StatusOK, nil
}
func (a *CursorAdapter) DiscoverModels(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return modelsFor("cursor", "auto", "gpt-5.6-sol", "gpt-5.5", "claude-sonnet-5", "claude-sonnet-4.6", "gemini-3.1-pro-preview"), nil
}
