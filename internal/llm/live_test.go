package llm

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Opt-in: never spend model quota as a side effect of the ordinary unit suite.
// Use only a synthetic tool; model-produced commands are never executed.
func TestLiveModels(t *testing.T) {
	if os.Getenv("AGENTFORGE_LIVE_MODELS") != "1" {
		t.Skip("live models not requested")
	}
	for _, key := range []string{"MODELSCOPE_ACCESS_TOKEN", "MODELSCOPE_BASE_URL", "MODELSCOPE_MODEL"} {
		if os.Getenv(key) == "" {
			t.Fatalf("required environment variable missing: %s", key)
		}
	}
	p, err := NewFromConfig(FactoryConfig{
		Provider:              "modelscope",
		ModelScopeBaseURL:     os.Getenv("MODELSCOPE_BASE_URL"),
		ModelScopeAccessToken: os.Getenv("MODELSCOPE_ACCESS_TOKEN"),
		ModelScopeModel:       os.Getenv("MODELSCOPE_MODEL"), ModelScopeTimeout: 90 * time.Second,
		ModelScopeMaxTokens: 4096,
	})
	if err != nil {
		t.Fatal("provider configuration failed (details suppressed)")
	}
	for _, model := range []string{os.Getenv("MODELSCOPE_MODEL")} {
		t.Run(model, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			consume := func(req Req) (string, []ToolCall, StopReason) {
				t.Helper()
				ch, err := p.Stream(ctx, req)
				if err != nil {
					// Provider error bodies can contain request/credential information.
					status := regexp.MustCompile(`openai http [0-9]{3}`).FindString(err.Error())
					t.Fatalf("model request failed (%T, %s); response details suppressed", err, status)
				}
				var text strings.Builder
				var calls []ToolCall
				var reason StopReason
				for ev := range ch {
					if ev.Err != nil {
						t.Fatal("model stream failed; details suppressed")
					}
					text.WriteString(ev.Token)
					calls = append(calls, ev.ToolCalls...)
					if ev.StopReason != "" {
						reason = ev.StopReason
					}
				}
				return text.String(), calls, reason
			}
			text, _, reason := consume(Req{Model: model, Messages: []Message{{Role: RoleUser, Content: "Reply with exactly P0_OK."}}})
			if !strings.Contains(text, "P0_OK") || reason != StopReasonStop {
				t.Fatal("text stream or stop reason did not satisfy the contract")
			}
			messages := []Message{{Role: RoleUser, Content: "Call p0_echo exactly once with value P0_TOOL. Do not answer until its result arrives. After it arrives, reply with its value."}}
			tools := []ToolDefinition{{Name: "p0_echo", Description: "Returns the supplied test value", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)}}
			text, calls, reason := consume(Req{Model: model, Messages: messages, Tools: tools})
			if len(calls) != 1 || calls[0].ID == "" || calls[0].Name != "p0_echo" || reason != StopReasonToolCalls {
				t.Fatal("streamed tool call identity/finish reason mismatch")
			}
			var args struct {
				Value string `json:"value"`
			}
			if json.Unmarshal([]byte(calls[0].Arguments), &args) != nil || args.Value != "P0_TOOL" {
				t.Fatal("tool arguments were not reassembled correctly")
			}
			messages = append(messages, Message{Role: RoleAssistant, Content: text, ToolCalls: calls}, Message{Role: RoleTool, ToolCallID: calls[0].ID, Content: `{"value":"P0_TOOL"}`})
			text, calls, reason = consume(Req{Model: model, Messages: messages})
			if !strings.Contains(text, "P0_TOOL") || len(calls) != 0 || reason != StopReasonStop {
				t.Fatal("tool result round trip failed")
			}
			t.Log("PASS: live text stream, tool arguments, call ID and result round trip")
		})
	}
}
