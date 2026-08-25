package llm

import "encoding/json"

func ExtractUsage(body []byte) Usage {
	var v struct {
		Usage Usage `json:"usage"`
	}
	_ = json.Unmarshal(body, &v)
	return v.Usage
}

func MergeUsage(payload string, current Usage) Usage {
	var v struct {
		Usage *Usage `json:"usage"`
	}
	if json.Unmarshal([]byte(payload), &v) == nil && v.Usage != nil {
		return *v.Usage
	}
	return current
}

func ContentDelta(payload string) string {
	var v struct {
		Choices []struct {
			Delta Delta `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal([]byte(payload), &v) != nil || len(v.Choices) == 0 {
		return ""
	}
	return v.Choices[0].Delta.Content
}

func FinishReason(payload string) string {
	var v struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal([]byte(payload), &v) != nil || len(v.Choices) == 0 {
		return ""
	}
	return v.Choices[0].FinishReason
}

func EstimatePromptTokensFromBody(body []byte) int64 {
	var req ChatRequest
	if json.Unmarshal(body, &req) != nil {
		return 0
	}
	return req.EstimatePromptTokens()
}

func IsDeterministicFromBody(body []byte) bool {
	var req ChatRequest
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	return IsDeterministic(&req)
}
