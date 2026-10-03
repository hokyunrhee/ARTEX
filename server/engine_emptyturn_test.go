package server

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

// Recognize and resume thinking-only turns with no text/tools; see steerHooks.Stop.

func assistantThinking(text string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
		{Type: llm.BlockThinking, Thinking: text, Signature: "sig"},
	}}
}

func TestIsThinkingOnlyTurn(t *testing.T) {
	toolUse := llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
		{Type: llm.BlockThinking, Thinking: "Scan ports first"},
		{Type: llm.BlockToolUse, ID: "t1", Name: "run_nuclei"},
	}}
	cases := []struct {
		name string
		msgs []llm.Message
		want bool
	}{
		{"thinking only", []llm.Message{llm.UserText("Start"), assistantThinking("Think")}, true},
		{"thinking and tool", []llm.Message{llm.UserText("Start"), toolUse}, false},
		{"thinking and text", []llm.Message{assistantThinking("Think"), {
			Role:    llm.RoleAssistant,
			Content: []llm.ContentBlock{{Type: llm.BlockThinking, Thinking: "x"}, llm.TextBlock("Conclusion")},
		}}, false},
		{"whitespace-only text", []llm.Message{{
			Role:    llm.RoleAssistant,
			Content: []llm.ContentBlock{{Type: llm.BlockThinking, Thinking: "x"}, llm.TextBlock("  \n ")},
		}}, true},
		{"empty assistant turn", []llm.Message{{Role: llm.RoleAssistant}}, true},
		// Tool results have the user role; inspect the preceding assistant message rather than misclassifying the last message.
		{"last message is a tool result", []llm.Message{toolUse, {
			Role:    llm.RoleUser,
			Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ToolUseID: "t1"}},
		}}, false},
		{"no assistant messages", []llm.Message{llm.UserText("Start")}, false},
		{"empty history", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isThinkingOnlyTurn(c.msgs); got != c.want {
				t.Fatalf("isThinkingOnlyTurn = %v, want %v", got, c.want)
			}
		})
	}
}

// fakeHooks is a programmable inner HookRunner verifying that steerHooks respects inner decisions.
type fakeHooks struct {
	prevent  bool
	blocking []string
	msg      string
}

func (f fakeHooks) PreToolUse(context.Context, string, []byte) (bool, string, []byte) {
	return false, "", nil
}
func (f fakeHooks) PostToolUse(context.Context, string, []byte, []byte, bool) {}
func (f fakeHooks) Stop(context.Context, []llm.Message) (bool, []string, string) {
	return f.prevent, f.blocking, f.msg
}

func TestSteerHooksStopNudgesEmptyTurn(t *testing.T) {
	empty := []llm.Message{assistantThinking("I should enumerate subdomains first")}

	t.Run("inject continuation for idle turns", func(t *testing.T) {
		h := steerHooks{nudges: &atomic.Int64{}, limit: defaultEmptyTurnNudges, label: "worker-1 · #1"}
		prevent, blocking, _ := h.Stop(context.Background(), empty)
		if prevent {
			t.Fatal("idle turns must not hard-stop")
		}
		if len(blocking) != 1 || blocking[0] != emptyTurnNudge {
			t.Fatalf("blocking = %v, want [emptyTurnNudge]", blocking)
		}
	})

	t.Run("no intervention for text or tools", func(t *testing.T) {
		h := steerHooks{nudges: &atomic.Int64{}, limit: defaultEmptyTurnNudges}
		normal := []llm.Message{{
			Role:    llm.RoleAssistant,
			Content: []llm.ContentBlock{llm.TextBlock("Scan complete; no open ports found")},
		}}
		if _, blocking, _ := h.Stop(context.Background(), normal); blocking != nil {
			t.Fatalf("normal completion mistaken for an idle turn: %v", blocking)
		}
		if n := h.nudges.Load(); n != 0 {
			t.Fatalf("counter changed without intervention, got %d", n)
		}
	})

	t.Run("allow completion after reaching limit", func(t *testing.T) {
		const limit = 5 // User configured five empty-response retries.
		h := steerHooks{nudges: &atomic.Int64{}, limit: limit}
		for i := 1; i <= limit; i++ {
			if _, blocking, _ := h.Stop(context.Background(), empty); len(blocking) != 1 {
				t.Fatalf("attempt %d should remain within allowance, blocking = %v", i, blocking)
			}
		}
		if _, blocking, _ := h.Stop(context.Background(), empty); blocking != nil {
			t.Fatalf("still injecting after limit: %v", blocking)
		}
	})

	// Empty-response retries of -1 disable this layer; emptyTurnNudgeLimit resolves to zero.
	t.Run("no intervention when disabled", func(t *testing.T) {
		h := steerHooks{nudges: &atomic.Int64{}, limit: 0}
		if _, blocking, _ := h.Stop(context.Background(), empty); blocking != nil {
			t.Fatalf("still injecting when disabled: %v", blocking)
		}
	})

	t.Run("do not override inner hard stop", func(t *testing.T) {
		h := steerHooks{inner: fakeHooks{prevent: true, msg: "guard prevents completion"}, nudges: &atomic.Int64{}, limit: defaultEmptyTurnNudges}
		prevent, blocking, msg := h.Stop(context.Background(), empty)
		if !prevent || msg != "guard prevents completion" || blocking != nil {
			t.Fatalf("inner hard stop changed: prevent=%v blocking=%v msg=%q", prevent, blocking, msg)
		}
		if n := h.nudges.Load(); n != 0 {
			t.Fatalf("deferring to inner must not consume allowance, got %d", n)
		}
	})

	t.Run("do not add to inner continuation", func(t *testing.T) {
		h := steerHooks{inner: fakeHooks{blocking: []string{"guard continuation reason"}}, nudges: &atomic.Int64{}, limit: defaultEmptyTurnNudges}
		_, blocking, _ := h.Stop(context.Background(), empty)
		if len(blocking) != 1 || blocking[0] != "guard continuation reason" {
			t.Fatalf("inner continuation message changed: %v", blocking)
		}
	})

	t.Run("preserve behavior without counter", func(t *testing.T) {
		h := steerHooks{limit: defaultEmptyTurnNudges} // For example, a future caller omits nudges.
		if _, blocking, _ := h.Stop(context.Background(), empty); blocking != nil {
			t.Fatalf("must not inject without counter: %v", blocking)
		}
	})
}
