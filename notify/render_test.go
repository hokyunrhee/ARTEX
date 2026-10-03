package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateBytesKeepsValidUTF8(t *testing.T) {
	// A key invariant: WeCom limits bytes, while Chinese characters use three UTF-8
	// bytes. Blind byte slicing can split a character and cause the platform to reject
	// the message. Mixed Chinese/English inputs of coprime lengths exercise every cut point.
	inputs := []string{
		"中文测试内容",
		"混合 mixed 内容 content",
		"a中b文c测d试e",
		"🔴🟠🟡🔵", // Four-byte emoji make incorrect boundaries especially visible.
		strings.Repeat("Finding", 100),
	}
	for _, in := range inputs {
		for max := 1; max <= len(in)+2; max++ {
			got := TruncateBytes(in, max)
			if !utf8.ValidString(got) {
				t.Fatalf("Input %q max=%d: produced invalid UTF-8 %q", in, max, got)
			}
			if len(got) > max {
				t.Fatalf("Input %q max=%d: result exceeds limit at %d bytes", in, max, len(got))
			}
			// Do not alter content when no truncation is needed.
			if len(in) <= max && got != in {
				t.Fatalf("Input %q max=%d: content changed without exceeding the limit -> %q", in, max, got)
			}
		}
	}
}

func TestTruncateBytesZeroMeansUnlimited(t *testing.T) {
	long := strings.Repeat("x", 10000)
	if got := TruncateBytes(long, 0); got != long {
		t.Fatal("max=0 must mean unlimited")
	}
	if got := TruncateBytes(long, -5); got != long {
		t.Fatal("max<0 must mean unlimited")
	}
}

func TestTruncateBytesEllipsisBudget(t *testing.T) {
	// When max is smaller than the ellipsis, appending it must not exceed the limit.
	got := TruncateBytes("abcdefgh", 1)
	if len(got) > 1 {
		t.Fatalf("max=1: result %q exceeds the limit at length %d", got, len(got))
	}
	// Normal truncation includes an ellipsis.
	if got := TruncateBytes("abcdefgh", 5); !strings.HasSuffix(got, ellipsis) {
		t.Fatalf("Expected an ellipsis, got %q", got)
	}
}

func TestTruncateRunesCountsCharactersNotBytes(t *testing.T) {
	// Preserve the difference from TruncateBytes: Telegram limits characters,
	// whereas counting bytes would cut Chinese messages to one third.
	s := "一二三四五六七八九十"
	got := TruncateRunes(s, 5)
	if n := utf8.RuneCountInString(got); n != 5 {
		t.Fatalf("Expected 5 characters, got %d (%q)", n, got)
	}
	// A byte limit must produce a substantially shorter result for the same text.
	if utf8.RuneCountInString(TruncateBytes(s, 5)) >= 5 {
		t.Fatal("Byte and character limits must not produce the same character count")
	}
}

func TestOneLineCollapsesWhitespace(t *testing.T) {
	got := OneLine("First line\n\nSecond line\twith tab   multiple spaces", 0)
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("All whitespace must be collapsed, got %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Fatalf("Consecutive spaces must not remain, got %q", got)
	}
	// Truncated output must remain readable and valid.
	got = OneLine("一二三四五六七八九十", 4)
	if n := utf8.RuneCountInString(got); n != 4 {
		t.Fatalf("Expected 4 characters, got %d (%q)", n, got)
	}
}

func TestTruncateHTMLNeverCutsTagInHalf(t *testing.T) {
	// Blind HTML truncation could leave `<a href="htt`, causing rejection of the whole message.
	s := `<b>Title</b>Body body body<a href="https://example.com/very/long/path">View details</a>`
	for max := 1; max <= utf8.RuneCountInString(s)+2; max++ {
		got := TruncateHTML(s, max)
		if n := utf8.RuneCountInString(got); max > 0 && n > max {
			t.Fatalf("max=%d: result exceeds limit at %d characters", max, n)
		}
		// No unmatched `<` may remain in the final segment.
		if lt := strings.LastIndex(got, "<"); lt >= 0 && !strings.Contains(got[lt:], ">") {
			t.Fatalf("max=%d: trailing tag was split -> %q", max, got)
		}
	}
}

func TestAssetLineOmitsExcess(t *testing.T) {
	if got := assetLine(nil, 3); got != "" {
		t.Fatalf("No assets must return an empty string, got %q", got)
	}
	if got := assetLine([]string{"a", "b"}, 3); got != "a, b" {
		t.Fatalf("All assets within the limit must be listed, got %q", got)
	}
	// Show the total when assets exceed the display limit so readers know some are omitted.
	got := assetLine([]string{"a", "b", "c", "d", "e"}, 2)
	if !strings.Contains(got, "5 assets total") {
		t.Fatalf("Expected total count 5, got %q", got)
	}
}

func TestSeverityAndStatusLabels(t *testing.T) {
	if AtLeast("", "low") {
		t.Fatal("Empty severity has ordinal 0 and must fail any threshold")
	}
	if !AtLeast("critical", "") {
		t.Fatal("An empty threshold must allow the event")
	}
	if got := StatusLabel("fixed"); got != "Fixed" {
		t.Fatalf("Unknown status mapping, got %q", got)
	}
	// Return unknown status values unchanged; do not invent labels.
	if got := StatusLabel("weird_status"); got != "weird_status" {
		t.Fatalf("Unknown status must be returned unchanged, got %q", got)
	}
}

// TestTruncateHTMLNeverCutsEntity covers a missed boundary: truncation must avoid
// partial HTML entities as well as tags. Cutting `&amp;` to `&amp` can make a strict
// parser reject the entire message, a substantial cost for common large digests.
func TestTruncateHTMLNeverCutsEntity(t *testing.T) {
	s := "aaaa&amp;bbbb&lt;cccc&quot;dddd"
	for max := 1; max <= utf8.RuneCountInString(s)+2; max++ {
		got := TruncateHTML(s, max)
		// Do not leave a trailing entity fragment with & but no matching semicolon.
		if amp := strings.LastIndex(got, "&"); amp >= 0 && !strings.Contains(got[amp:], ";") {
			t.Fatalf("max=%d: trailing entity fragment %q", max, got[amp:])
		}
		if strings.Contains(got, "&amp\x00") {
			t.Fatalf("max=%d: malformed entity", max)
		}
	}
}
