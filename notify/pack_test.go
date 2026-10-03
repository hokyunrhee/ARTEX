package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Whole-item digest packing. When a digest exceeds the channel limit, retain
// whole items and accurately report the omitted count so callers mark only
// actually delivered entries sent.
//
// Previously the entire rendered body was truncated and the whole batch marked
// delivered. Tail findings disappeared while delivery history falsely showed success.

func TestMarkdownBodyPacksWholeItemsWithinByteLimit(t *testing.T) {
	// A 200-item digest must exceed the WeCom 4096-byte limit.
	m := batchMsg(200)
	body, kept := markdownBody(m, weComMarkdownLimit)

	if len(body) > weComMarkdownLimit {
		t.Fatalf("Body size %d bytes exceeds limit %d", len(body), weComMarkdownLimit)
	}
	if !utf8.ValidString(body) {
		t.Fatal("Body is not valid UTF-8")
	}
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("Expected a partial batch (0 < kept < %d), got %d", len(m.Items), kept)
	}
	// The header must state how many items are included and how many remain,
	// otherwise readers may mistake its count for the complete set.
	if !strings.Contains(body, "remaining") || !strings.Contains(body, "will follow in the next message") {
		t.Fatalf("Header must explain how many items are omitted from this message:\n%s", body[:minInt(400, len(body))])
	}
	// Only the first kept items belong in this message.
	for i := 0; i < kept; i++ {
		if !strings.Contains(body, "Finding"+itoa(i+1)) {
			t.Fatalf("Item %d must appear in this message:\n%s", i+1, body)
		}
	}
	if strings.Contains(body, "Finding"+itoa(kept+1)) {
		t.Fatalf("Item %d must not appear because it belongs to the next batch", kept+1)
	}
}

func TestMarkdownBodyKeepsEverythingWhenUnderLimit(t *testing.T) {
	m := batchMsg(3)
	body, kept := markdownBody(m, 0) // 0 means unlimited.
	if kept != len(m.Items) {
		t.Fatalf("Unlimited length must retain every item, got kept=%d", kept)
	}
	if strings.Contains(body, "remaining") {
		t.Fatalf("An untruncated message must not display a truncation notice:\n%s", body)
	}
}

func TestMarkdownBodyAlwaysKeepsAtLeastOneItem(t *testing.T) {
	// Even if one item exceeds the budget, send one and let final truncation handle it.
	// Otherwise an oversized finding would block the batch forever, repeatedly claimed
	// but never small enough to send.
	m := batchMsg(5)
	_, kept := markdownBody(m, 50)
	if kept != 1 {
		t.Fatalf("At least 1 item must be retained, got %d", kept)
	}
}

func TestMarkdownBodySingleReturnsOne(t *testing.T) {
	_, kept := markdownBody(singleMsg(), 4096)
	if kept != 1 {
		t.Fatalf("A single-item message must report 1 delivered item, got %d", kept)
	}
	// An empty message has no deliverable items.
	if _, k := markdownBody(Message{}, 4096); k != 0 {
		t.Fatalf("An empty message must report 0 items, got %d", k)
	}
}

func TestTelegramPackingUsesRuneBudget(t *testing.T) {
	m := batchMsg(200)
	text, kept := telegramHTML(m)
	// Telegram limits characters; counting bytes would reduce Chinese messages to one third.
	if n := utf8.RuneCountInString(text); n > telegramTextLimit {
		t.Fatalf("Body length %d characters exceeds limit %d", n, telegramTextLimit)
	}
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("Expected only part of the batch, got %d", kept)
	}
	if !strings.Contains(text, "will follow in the next message") {
		t.Fatalf("Message must explain that more items remain:\n%.300s", text)
	}
}

func TestFeishuPackingReportsKept(t *testing.T) {
	m := batchMsg(2000)
	_, kept := feishuCard(m)
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("Card must contain only part of the batch, got %d", kept)
	}
}

func TestWebhookAndEmailReportAllItems(t *testing.T) {
	// These two channels do not truncate the body, so the entire batch is delivered.
	m := batchMsg(7)
	if n := len(m.Items); n != 7 {
		t.Fatal("Precondition failed")
	}
	// Verify indirectly through the renderer result: markdownBody(0) retains every item.
	if _, k := markdownBody(m, 0); k != len(m.Items) {
		t.Fatalf("Unlimited length must include all items, got %d", k)
	}
}

// TestMarkdownEscapesUntrustedContent ensures untrusted data cannot change
// message structure. Titles and summaries come from model output informed by
// target responses; asset names come from target URLs.
func TestMarkdownEscapesUntrustedContent(t *testing.T) {
	cases := []struct {
		name  string
		item  Item
		must  []string // Required escaped form.
		wrong []string // Forbidden unescaped form.
	}{
		{
			name: "Newline and external link in title",
			item: Item{
				Severity: "high",
				Name:     "Login SQL injection\n[Urgent: verify your account](http://attacker.tld)",
			},
			// Collapse newlines to prevent forged list items or quotes; escape square and
			// round brackets to prevent clickable external links.
			must:  []string{`\[Urgent: verify your account\]`, `\(http://attacker.tld\)`},
			wrong: []string{"\n[Urgent", "\n\n[Urgent"},
		},
		{
			name: "Image beacon in title",
			item: Item{
				Severity: "high",
				Name:     "Finding ![](http://attacker.tld/beacon)",
			},
			must:  []string{`\!`, `\(http://attacker.tld/beacon\)`},
			wrong: []string{"![]("},
		},
		{
			name: "Emphasis and quote in asset name",
			item: Item{
				Severity: "high",
				Name:     "Plain title",
				Assets:   []string{"a.com/*injection*>quote"},
			},
			must:  []string{`\*injection\*`, `\>`},
			wrong: []string{"*injection*"},
		},
		{
			name: "Backticks and pipe in summary",
			item: Item{
				Severity: "high",
				Name:     "Title",
				Summary:  "`code` | table",
			},
			must:  []string{"\\`code\\`", `\|`},
			wrong: []string{"`code`"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Message{Items: []Item{tc.item}}
			// Single-item writeItem is the rendering path shared by the three Markdown channels.
			var b strings.Builder
			writeItem(&b, tc.item, "", true)
			got := b.String()
			for _, want := range tc.must {
				if !strings.Contains(got, want) {
					t.Errorf("Missing escaped form %q:\n%s", want, got)
				}
			}
			for _, bad := range tc.wrong {
				if strings.Contains(got, bad) {
					t.Errorf("Unescaped form %q permits structural or external-link injection:\n%s", bad, got)
				}
			}
			_ = m
		})
	}
}

// TestMarkdownEscapeBackslashFirst fixes the escaping order: escape backslashes
// first, or later processing will double newly inserted escapes.
func TestMarkdownEscapeBackslashFirst(t *testing.T) {
	if got := markdownEscape(`a\b*c`); got != `a\\b\*c` {
		t.Fatalf("Incorrect escaping order, got %q", got)
	}
}

// TestTelegramTitleHasNoMarkdownEscapes guards against Markdown escapes leaking
// into Telegram HTML. Escaping in a shared title function previously displayed
// visible backslashes such as `\(1\)` in Telegram messages.
func TestTelegramTitleHasNoMarkdownEscapes(t *testing.T) {
	m := Message{Items: []Item{{Severity: "high", Name: "alert(1) *emphasis*"}}}
	text, _ := telegramHTML(m)
	if strings.Contains(text, `\(`) || strings.Contains(text, `\*`) {
		t.Fatalf("Telegram body contains Markdown backslash escapes:\n%s", text)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
