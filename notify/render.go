package notify

import (
	"strings"
	"unicode/utf8"
)

const ellipsis = "…"

// TruncateBytes truncates s to at most max bytes, guaranteeing the result is
// valid UTF-8 and doesn't cut a character in half.
//
// Why it must cut on a character boundary: the WeCom group bot's markdown has a
// hard 4096-**byte** cap (not a character count), and a CJK character is 3 bytes.
// Slicing by raw bytes would cut a character in half and produce invalid UTF-8 —
// the platform either rejects the whole message or shows mojibake boxes. The
// approach here is to back up from the budget position to the nearest rune start
// byte (utf8.RuneStart identifies a continuation byte 0b10xxxxxx).
//
// max<=0 means no limit. An ellipsis is appended after truncation, unless max is
// too small to fit the ellipsis.
func TruncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	budget := max - len(ellipsis)
	suffix := ellipsis
	if budget < 0 {
		// max is shorter than the ellipsis itself: drop the ellipsis and truncate
		// plainly, so the result doesn't end up exceeding max.
		budget = max
		suffix = ""
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// OneLine collapses multi-line text onto a single line: fold all whitespace, then
// truncate by character count. Used for the title line of an IM message —
// summaries often contain newlines, and dropping those straight into a
// table/title breaks the layout.
// max<=0 means no length limit.
func OneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	return TruncateRunes(s, max)
}

// TruncateRunes truncates s to at most max characters (not bytes), appending an
// ellipsis when it overflows. max<=0 means no limit.
//
// The difference from TruncateBytes is the platform's unit: WeCom caps by bytes,
// Telegram caps by character count. Using the wrong unit doesn't error — it just
// cuts the message far shorter than intended (a CJK char = 3 bytes, so a 4096
// byte cut leaves only ~1365 chars), so both functions must be kept and chosen
// per channel.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + ellipsis
}

// TruncateHTML truncates an HTML fragment by character count and guarantees it
// doesn't produce a half-finished tag.
//
// Truncating HTML by characters directly can cut a broken tag like
// `<a href="htt`, and the platform parser either errors and rejects the whole
// message or swallows the following body as an attribute value. The approach
// here is: truncate by characters first, then check whether the tail has an
// unclosed `<`, and if so back up to before it.
//
// Tag balancing (completing a `</b>` and such) is not done: Telegram's HTML
// parser auto-closes unclosed tags, and implementing balancing oneself would have
// to handle quotes in attributes, comments, and self-closing tags — complexity
// out of proportion to the benefit.
func TruncateHTML(s string, max int) string {
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	cut := TruncateRunes(s, max)
	// If the tail is a `<`-led fragment (the last `<` has no following `>`), back
	// up to before that `<`.
	if lt := strings.LastIndex(cut, "<"); lt >= 0 && !strings.Contains(cut[lt:], ">") {
		cut = cut[:lt]
	}
	// If the tail is a cut-off HTML entity (such as `&amp;` cut to `&amp`), back
	// up the same way. An entity fragment can get the **whole message** rejected
	// by a parser that only accepts entities — a digest message over the length
	// cap is common, not worth losing the whole notification over.
	if amp := strings.LastIndex(cut, "&"); amp >= 0 && !strings.Contains(cut[amp:], ";") {
		cut = cut[:amp]
	}
	return cut
}

// packItemCount computes how many items fit **completely** within the budget,
// for packing a digest message whole-item by whole-item.
//
// Why pack whole items rather than render the whole thing then truncate:
// truncation makes the back-half items vanish, while their delivery records are
// still marked delivered — not visible in the message, not visible in the
// delivery history, so the finding is just gone. By packing whole items, the
// ones that don't fit stay in the database as the next batch, and the kept the
// caller receives is the number this message genuinely delivered.
//
// Parameters: maxSize<=0 means no limit; reserve is the amount set aside for the
// message header/footer; size does the measuring (the unit differs per platform:
// WeCom/DingTalk by bytes, Telegram by character count — using the wrong unit
// doesn't error, it just compresses a CJK message far below the cap); render
// renders item idx into its actual text — the length varies by content and can't
// be estimated.
//
// Returns at least 1 (as long as there are items). Even one extreme over-long
// item must send that single item, backstopped by the caller's final truncation,
// or one over-long finding would jam the whole batch in place forever.
func packItemCount(items []Item, maxSize, reserve int, footer string, size func(string) int, render func(Item, int) string) int {
	if maxSize <= 0 {
		return len(items)
	}
	budget := maxSize - reserve - size(footer)
	if budget < 0 {
		budget = 0
	}
	used := 0
	for i, it := range items {
		used += size(render(it, i))
		if used > budget && i > 0 {
			return i
		}
	}
	return len(items)
}

// byteSize / runeSize are packItemCount's two measuring units, named so that
// call sites don't carry a bare func(s string) int closure, which would make it
// hard to see at a glance which unit is in use.
func byteSize(s string) int { return len(s) }
func runeSize(s string) int { return utf8.RuneCountInString(s) }

// assetLine renders an asset list to a one-line display string, omitting the
// rest and noting the total when there are more than limit. A finding may anchor
// dozens of assets, and listing them all would swamp the message.
func assetLine(assets []string, limit int) string {
	if len(assets) == 0 {
		return ""
	}
	if limit <= 0 || len(assets) <= limit {
		return strings.Join(assets, ", ")
	}
	return strings.Join(assets[:limit], ", ") + " and " + itoa(len(assets)) + " total"
}

// itoa is a short alias for strconv.Itoa, used only for building display text, to
// avoid importing strconv everywhere.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
