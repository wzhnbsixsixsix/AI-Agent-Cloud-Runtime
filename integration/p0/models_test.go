package p0

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Compile the selected official MCP SDK alongside Eino; this is not an
// assertion that MCP authorization or the future Eino bridge is implemented.
func TestSelectedSDKsCompile(t *testing.T) {
	client := mcp.NewClient(&mcp.Implementation{Name: "agentforge-p0", Version: "0.1.0"}, nil)
	if client == nil {
		t.Fatal("nil MCP client")
	}
}

func TestLiveEinoModels(t *testing.T) {
	if os.Getenv("AGENTFORGE_LIVE_MODELS") != "1" {
		t.Skip("live models not requested")
	}
	for _, vendor := range []struct{ name, base, key, model string }{
		{"Qwen", "MODELSCOPE_BASE_URL", "MODELSCOPE_ACCESS_TOKEN", "MODELSCOPE_MODEL"},
	} {
		t.Run(vendor.name, func(t *testing.T) {
			for _, key := range []string{vendor.base, vendor.key, vendor.model} {
				if os.Getenv(key) == "" {
					t.Fatalf("missing %s", key)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			maxTokens := 4096
			config := &openai.ChatModelConfig{BaseURL: os.Getenv(vendor.base), APIKey: os.Getenv(vendor.key), Model: os.Getenv(vendor.model), Timeout: 90 * time.Second, MaxTokens: &maxTokens}
			cm, err := openai.NewChatModel(ctx, config)
			if err != nil {
				t.Fatal("Eino configuration failed; details suppressed")
			}
			consume := func(cm model.ToolCallingChatModel, messages []*schema.Message) *schema.Message {
				t.Helper()
				stream, err := cm.Stream(ctx, messages)
				if err != nil {
					var apiErr *openai.APIError
					if errors.As(err, &apiErr) {
						t.Fatalf("Eino model request failed: HTTP %d; details suppressed", apiErr.HTTPStatusCode)
					}
					t.Fatalf("Eino model request failed (%T); details suppressed", err)
				}
				defer stream.Close()
				var chunks []*schema.Message
				for {
					chunk, err := stream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal("Eino stream failed; details suppressed")
					}
					chunks = append(chunks, chunk)
				}
				message, err := schema.ConcatMessages(chunks)
				if err != nil {
					t.Fatal("Eino fragment assembly failed")
				}
				return message
			}
			text := consume(cm, []*schema.Message{schema.UserMessage("Reply with exactly P0_OK.")})
			if !strings.Contains(text.Content, "P0_OK") {
				t.Fatal("text stream mismatch")
			}
			toolModel, err := cm.WithTools([]*schema.ToolInfo{{Name: "p0_echo", Desc: "Returns the supplied test value", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"value": {Type: schema.String, Required: true}})}})
			if err != nil {
				t.Fatal("tool binding failed")
			}
			messages := []*schema.Message{schema.UserMessage("Call p0_echo exactly once with value P0_TOOL. Wait for its result before answering, then reply with the value.")}
			call := consume(toolModel, messages)
			if len(call.ToolCalls) != 1 || call.ToolCalls[0].ID == "" || call.ToolCalls[0].Function.Name != "p0_echo" {
				t.Fatal("tool call contract failed")
			}
			var args struct {
				Value string `json:"value"`
			}
			if json.Unmarshal([]byte(call.ToolCalls[0].Function.Arguments), &args) != nil || args.Value != "P0_TOOL" {
				t.Fatal("tool arguments mismatch")
			}
			messages = append(messages, call, schema.ToolMessage(`{"value":"P0_TOOL"}`, call.ToolCalls[0].ID))
			answer := consume(cm, messages)
			if !strings.Contains(answer.Content, "P0_TOOL") || len(answer.ToolCalls) != 0 {
				t.Fatal("tool result round trip failed")
			}
			t.Log("PASS: Eino official model component, live stream and tool round trip")
		})
	}
}
