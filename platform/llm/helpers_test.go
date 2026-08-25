package llm

import (
	"encoding/json"
	"testing"
)

func TestIsDeterministic(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"defaults", `{"model":"m","messages":[{"role":"user","content":"hi"}]}`, true},
		{"greedy explicit", `{"model":"m","temperature":0,"top_p":1,"n":1,"frequency_penalty":0,"presence_penalty":0,"messages":[{"role":"user","content":"hi"}]}`, true},
		{"temperature", `{"model":"m","temperature":0.1,"messages":[{"role":"user","content":"hi"}]}`, false},
		{"top p", `{"model":"m","top_p":0.9,"messages":[{"role":"user","content":"hi"}]}`, false},
		{"multiple choices", `{"model":"m","n":2,"messages":[{"role":"user","content":"hi"}]}`, false},
		{"frequency penalty", `{"model":"m","frequency_penalty":1,"messages":[{"role":"user","content":"hi"}]}`, false},
		{"presence penalty", `{"model":"m","presence_penalty":1,"messages":[{"role":"user","content":"hi"}]}`, false},
		{"tools", `{"model":"m","tools":[{"type":"function","function":{"name":"f"}}],"messages":[{"role":"user","content":"hi"}]}`, false},
		{"tool choice", `{"model":"m","tool_choice":"none","messages":[{"role":"user","content":"hi"}]}`, false},
		{"prior tool call", `{"model":"m","messages":[{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"f"}}]}]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req ChatRequest
			if err := json.Unmarshal([]byte(tt.body), &req); err != nil {
				t.Fatal(err)
			}
			if got := IsDeterministic(&req); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}
