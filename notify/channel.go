package notify

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Channel adapts a notification destination. Implementations must be stateless:
// the same instance serves multiple channel configurations concurrently, and all
// credentials must come from cfg.
type Channel interface {
	// Kind returns the channel type identifier, matching its registry key.
	Kind() string
	// Validate checks required fields and formats when saving configuration. Errors
	// are shown directly to the operator, so identify the missing field rather than
	// returning a generic invalid-configuration message.
	Validate(cfg map[string]any) error
	// Send delivers one message and returns the number of items actually delivered.
	//
	// Platforms limit message length, so a digest may not fit the entire batch. If the
	// caller marked the whole batch as sent, truncated items would disappear from both
	// the message and the delivery history without any indication they were omitted.
	// With kept, the caller marks only the first kept items as sent and defers the rest.
	//
	// An error means delivery failed; *PermanentError means it must not be retried.
	// On failure, kept has no meaning and the caller should ignore it.
	Send(ctx context.Context, cfg map[string]any, m Message) (int, error)
	// DefaultRatePerMin returns the officially recommended per-minute delivery limit
	// as the default for new channel configurations. Zero means no known limit.
	DefaultRatePerMin() int
	// SecretKeys returns configuration fields containing credentials. The API masks
	// them in responses and preserves stored values when updates contain masked values.
	// Only the implementation knows what is secret (the entire WeCom Webhook URL is a
	// credential, while DingTalk also has a separate secret), so callers must not guess.
	SecretKeys() []string
	// DestinationKeys returns configuration fields that determine where messages go.
	//
	// Like SecretKeys, this is security-sensitive: destination and credential fields
	// are independent. Allowing a destination-only change while retaining credentials
	// would let anyone who can edit a channel send its stored credentials to their own
	// server, defeating response masking. See PrepareConfigUpdate.
	DestinationKeys() []string
}

// registry explicitly lists channel adapters instead of using init registration.
// This keeps supported channels visible in one place and exposes missing additions
// at compile time instead of depending on runtime side effects.
var registry = map[string]Channel{
	KindDingTalk: dingTalkChannel{},
	KindFeishu:   feishuChannel{},
	KindWeCom:    weComChannel{},
	KindWebhook:  webhookChannel{},
	KindTelegram: telegramChannel{},
	KindEmail:    emailChannel{},
}

// Get returns the adapter for a channel type.
func Get(kind string) (Channel, bool) {
	c, ok := registry[kind]
	return c, ok
}

// ValidKind reports whether kind is a supported channel type.
func ValidKind(kind string) bool {
	_, ok := registry[kind]
	return ok
}

// Kinds returns all supported types in lexical order for a stable UI selector.
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PermanentError marks a delivery failure that must not be retried, such as invalid
// credentials, target rejection, or an invalid request body. Retries help transient
// network, rate-limit, or remote 5xx failures; repeatedly backing off permanent
// failures cannot succeed and buries the actual problem in retry logs.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent marks err as a permanent failure. A nil err returns nil so callers can
// write return Permanent(someCheck()).
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent reports whether the error chain contains a permanent-failure marker.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}

// Configuration-reading helpers.
//
// Channel configuration comes from a JSONB column, decoded as map[string]any:
// numbers are float64 and arrays are []any. These helpers centralize conversion
// and tolerate form input variations, such as a port submitted as a string.

// cfgString reads a string value and trims whitespace commonly introduced by
// copying and pasting into web forms.
func cfgString(cfg map[string]any, key string) string {
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// cfgInt reads an integer from float64 (the JSON default) or a string.
func cfgInt(cfg map[string]any, key string) int {
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// cfgBool reads a boolean, also accepting the strings "true" and "1".
func cfgBool(cfg map[string]any, key string) bool {
	switch v := cfg[key].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

// cfgStrings reads a string array, trimming whitespace and dropping empty entries.
func cfgStrings(cfg map[string]any, key string) []string {
	raw, ok := cfg[key].([]any)
	if !ok {
		// Also accept a single string for forms submitting just one value.
		if s := cfgString(cfg, key); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cfgMap reads a string map, such as custom HTTP headers, trims keys and values,
// and discards empty keys.
func cfgMap(cfg map[string]any, key string) map[string]string {
	raw, ok := cfg[key].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = s
	}
	return out
}
