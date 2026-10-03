package notify

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Channel is an adapter for one notification channel. Implementations must be
// **stateless**: a single instance is reused concurrently across many channel
// configs, and every credential is passed in via the cfg parameter.
type Channel interface {
	// Kind returns the channel type identifier, which must match the registry key.
	Kind() string
	// Validate is called when a config is saved; it checks required fields and
	// format. The returned error is shown directly to the person configuring the
	// channel, so the text must say "which field is missing" rather than a vague
	// "invalid config".
	Validate(cfg map[string]any) error
	// Send delivers one message and returns the **number of items actually
	// delivered** plus an error.
	//
	// Why return a count: every platform has a message length cap, and a digest
	// message that doesn't fit the whole batch gets truncated. If the caller
	// unconditionally marks the whole batch as delivered, the cut-off items just
	// vanish — they're not in the message, the delivery history still shows
	// success, and there is nowhere to discover that a finding was never sent.
	// By returning kept, the caller marks only the first kept items and leaves
	// the rest for the next batch.
	//
	// A returned error means delivery failed; a *PermanentError means it should
	// not be retried. On failure kept is meaningless and the caller should ignore
	// it.
	Send(ctx context.Context, cfg map[string]any, m Message) (int, error)
	// DefaultRatePerMin returns the platform's officially recommended per-minute
	// delivery cap, used as the default rate limit for a newly created channel
	// instance. Returning 0 means no known limit.
	DefaultRatePerMin() int
	// SecretKeys returns the key names in this channel's config that are
	// credentials. On API echo these keys' values are masked, and on update a
	// masked value means the stored value is kept. Only the implementation itself
	// knows which fields count as credentials (for WeCom the entire webhook URL is
	// the credential, whereas for DingTalk it's only the secret within), so this
	// knowledge must come from the channel and cannot be guessed by a higher
	// layer.
	SecretKeys() []string
	// DestinationKeys returns the key names in this channel's config that decide
	// "where the message is sent".
	//
	// Like SecretKeys, this is security-relevant: the destination address and the
	// credentials are two independent sets of fields. If "change only the address,
	// keep the credentials as-is" were allowed, anyone who can edit a channel
	// config could send the real stored credentials to a server they control,
	// rendering the channel config's masking completely meaningless.
	// See PrepareConfigUpdate for details.
	DestinationKeys() []string
}

// registry is the channel registry. Deliberately an explicit literal rather than
// init() self-registration: this way "which channels exist" is visible in one
// place, and adding a channel surfaces any omission at compile time rather than
// via a runtime side effect.
var registry = map[string]Channel{
	KindDingTalk: dingTalkChannel{},
	KindFeishu:   feishuChannel{},
	KindWeCom:    weComChannel{},
	KindWebhook:  webhookChannel{},
	KindTelegram: telegramChannel{},
	KindEmail:    emailChannel{},
}

// Get returns the channel implementation for a type.
func Get(kind string) (Channel, bool) {
	c, ok := registry[kind]
	return c, ok
}

// ValidKind reports whether kind is a supported channel type.
func ValidKind(kind string) bool {
	_, ok := registry[kind]
	return ok
}

// Kinds returns all supported channel types in lexical order (for a stable UI
// dropdown).
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PermanentError marks a delivery failure that should not be retried: bad
// credentials, destination rejection, an invalid request body, and so on.
// Retrying only makes sense for transient faults (network jitter, rate limiting,
// a 5xx from the peer); backing off and retrying a permanent failure will never
// succeed and only drowns the real error in the retry log.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent marks err as a permanent failure. It returns nil when err is nil, so
// it can be written as `return Permanent(someCheck())`.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent reports whether the err chain carries a permanent-failure marker.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}

// ---- config-reading helpers ----
//
// Channel config comes from a JSONB column in the database; after encoding/json
// unmarshalling it is a map[string]any, with numbers always float64 and arrays
// always []any. The helpers below unify that layer of conversion and tolerate
// the type drift that a user leaving a field blank in the UI can cause (such as
// filling a port in as a string).

// cfgString reads a string config item and trims surrounding whitespace — it's
// very easy to pick some up when copy-pasting from a web form.
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

// cfgInt reads an integer config item, accepting both float64 (the JSON default)
// and string sources.
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

// cfgBool reads a boolean config item, also accepting the strings "true"/"1".
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

// cfgStrings reads a string-array config item, trimming whitespace and dropping
// empty strings.
func cfgStrings(cfg map[string]any, key string) []string {
	raw, ok := cfg[key].([]any)
	if !ok {
		// Also accept a single string, to ease form submission when there's only
		// one value.
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

// cfgMap reads a string-map config item (such as custom HTTP headers), trimming
// whitespace from keys and values and dropping empty keys.
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
