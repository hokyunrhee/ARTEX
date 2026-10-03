package notify

import (
	"errors"
	"strings"
	"testing"
)

func TestMaskedValueHidesBodyButKeepsTailHint(t *testing.T) {
	const secret = "https://oapi.dingtalk.com/robot/send?access_token=abcdef123456"
	got := MaskedValue(secret)
	if strings.Contains(got, "abcdef123456") {
		t.Fatalf("Masked value exposes the full credential: %q", got)
	}
	if strings.Contains(got, "oapi.dingtalk.com") {
		t.Fatalf("Masked value must not expose the address body: %q", got)
	}
	// Retain the last 6 characters so users can recognize the bot.
	if !strings.HasSuffix(got, "123456") {
		t.Fatalf("Expected the last 6 characters as a recognition hint: %q", got)
	}
	if !IsMasked(got) {
		t.Fatalf("IsMasked must recognize the masked value: %q", got)
	}
}

func TestMaskedValueShortSecretGivesNoHint(t *testing.T) {
	// Exposing 6 trailing characters of a short credential could reveal the whole value.
	for _, s := range []string{"abc", "abcdef", ""} {
		got := MaskedValue(s)
		if got != MaskedPrefix {
			t.Fatalf("A credential of length %d must not expose a suffix hint, got %q", len(s), got)
		}
		if s != "" && strings.Contains(got, s) {
			t.Fatalf("Masked value contains the original: %q", got)
		}
	}
}

func TestMaskConfigMasksOnlySecrets(t *testing.T) {
	cfg := map[string]any{
		"webhook": "https://example.com/hook?token=SECRETVALUE",
		"secret":  "SECtest123456",
		"port":    float64(587),
		"host":    "smtp.example.com",
	}
	masked := MaskConfig(KindDingTalk, cfg)
	for _, k := range []string{"webhook", "secret"} {
		s, _ := masked[k].(string)
		if !IsMasked(s) {
			t.Errorf("%s must be masked, got %q", k, s)
		}
	}
	// Preserve non-credential fields for the UI.
	if masked["port"] != float64(587) {
		t.Errorf("Non-credential field port must remain unchanged: %v", masked["port"])
	}
}

func TestMaskConfigUnknownKindReturnsEmpty(t *testing.T) {
	// For unknown channel types, return an empty configuration rather than potentially exposing credentials.
	got := MaskConfig("nope", map[string]any{"webhook": "https://x/y?token=LEAK"})
	if len(got) != 0 {
		t.Fatalf("Unknown channel types must return an empty configuration, got %v", got)
	}
}

func TestMaskConfigDoesNotMutateInput(t *testing.T) {
	// Masking is presentation-only and must never change stored credentials.
	cfg := map[string]any{"webhook": "https://example.com/hook", "secret": "SECtest123456"}
	_ = MaskConfig(KindDingTalk, cfg)
	if IsMasked(cfg["secret"].(string)) {
		t.Fatal("MaskConfig mutated its input, which would overwrite real credentials with masked values")
	}
}

func TestMergeConfigKeepsStoredOnMaskedIncoming(t *testing.T) {
	stored := map[string]any{"webhook": "https://real/hook", "secret": "REALSECRET", "method": "POST"}
	// The user changes only method; the browser submits the masked value and new method.
	incoming := map[string]any{
		"webhook": MaskedValue("https://real/hook"),
		"secret":  MaskedValue("REALSECRET"),
		"method":  "PUT",
	}
	got := MergeConfig(stored, incoming)
	if got["webhook"] != "https://real/hook" || got["secret"] != "REALSECRET" {
		t.Fatalf("Masked fields must retain their stored values, got %v", got)
	}
	if got["method"] != "PUT" {
		t.Fatalf("Edited fields must take effect, got %v", got["method"])
	}
}

func TestMergeConfigEmptyStringClears(t *testing.T) {
	stored := map[string]any{"webhook": "https://real/hook", "secret": "REALSECRET"}
	got := MergeConfig(stored, map[string]any{"secret": ""})
	if _, ok := got["secret"]; ok {
		t.Fatalf("An empty string must clear the field, got %v", got)
	}
	// Preserve omitted fields for partial updates.
	if got["webhook"] != "https://real/hook" {
		t.Fatalf("Omitted fields must be preserved, got %v", got)
	}
}

func TestMergeConfigKeepsUnmentionedStoredKeys(t *testing.T) {
	stored := map[string]any{"host": "smtp.example.com", "port": float64(587), "password": "pw"}
	got := MergeConfig(stored, map[string]any{"port": float64(465)})
	if got["host"] != "smtp.example.com" || got["password"] != "pw" {
		t.Fatalf("Omitted fields must be preserved, got %v", got)
	}
	if got["port"] != float64(465) {
		t.Fatalf("Supplied fields must be updated, got %v", got["port"])
	}
}

// TestPrepareConfigUpdateBlocksDestinationSwap enforces a key security invariant:
// changing the destination must never carry existing credentials to the new address.
//
// These cases use attack-shaped input: change the address and omit credentials.
// Testing only legitimate input would pass even if the protection stopped working.
func TestPrepareConfigUpdateBlocksDestinationSwap(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		stored   map[string]any
		incoming map[string]any
		// wantMissing identifies the credential key expected in the error.
		wantMissing string
	}{
		{
			name: "Generic Webhook changes URL while retaining Authorization header",
			kind: KindWebhook,
			stored: map[string]any{
				"url":     "https://legit.example.com/hook",
				"headers": map[string]any{"Authorization": "Bearer REAL-TOKEN"},
			},
			incoming:    map[string]any{"url": "https://attacker.tld/c"},
			wantMissing: "headers",
		},
		{
			name:        "Telegram changes base_url to send Bot Token to another endpoint",
			kind:        KindTelegram,
			stored:      map[string]any{"bot_token": "123456:REAL", "chat_id": "1", "base_url": "https://api.telegram.org"},
			incoming:    map[string]any{"base_url": "https://attacker.tld"},
			wantMissing: "bot_token",
		},
		{
			name:        "Email changes SMTP host while retaining password",
			kind:        KindEmail,
			stored:      map[string]any{"host": "smtp.corp.com", "port": 587, "password": "REALPW", "from": "a@b.c", "to": []any{"d@e.f"}},
			incoming:    map[string]any{"host": "smtp.attacker.tld"},
			wantMissing: "password",
		},
		{
			name:        "Disabling email TLS also requires explicit password confirmation",
			kind:        KindEmail,
			stored:      map[string]any{"host": "smtp.corp.com", "port": 587, "tls": false, "password": "REALPW", "from": "a@b.c", "to": []any{"d@e.f"}},
			incoming:    map[string]any{"tls": true},
			wantMissing: "password",
		},
		{
			// A masked value means reuse the old credential, which must also be rejected on destination changes.
			name:        "Resubmitted masked credential with a new address",
			kind:        KindTelegram,
			stored:      map[string]any{"bot_token": "123456:REAL", "chat_id": "1", "base_url": "https://api.telegram.org"},
			incoming:    map[string]any{"base_url": "https://attacker.tld", "bot_token": MaskedValue("123456:REAL")},
			wantMissing: "bot_token",
		},
		{
			name:        "DingTalk changes Webhook while retaining signing secret",
			kind:        KindDingTalk,
			stored:      map[string]any{"webhook": "https://oapi.dingtalk.com/robot/send?access_token=OLD", "secret": "REALSEC"},
			incoming:    map[string]any{"webhook": "https://attacker.tld/hook"},
			wantMissing: "secret",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged, err := PrepareConfigUpdate(tc.kind, tc.stored, tc.incoming)
			if err == nil {
				t.Fatalf("An address change without explicitly supplied credentials must be rejected; got configuration %v", merged)
			}
			var target *ErrDestinationChangedWithoutCredentials
			if !errors.As(err, &target) {
				t.Fatalf("Expected a dedicated error type for actionable API feedback, got %T: %v", err, err)
			}
			found := false
			for _, m := range target.Missing {
				if m == tc.wantMissing {
					found = true
				}
			}
			if !found {
				t.Fatalf("Error must name missing credential key %q, got %v", tc.wantMissing, target.Missing)
			}
			// The error must explain how the operator can fix the configuration.
			if !strings.Contains(err.Error(), tc.wantMissing) {
				t.Errorf("Error must mention %q: %v", tc.wantMissing, err)
			}
		})
	}
}

// TestPrepareConfigUpdateAllowsLegitimateEdits checks the inverse: valid edits
// must work, or users may bypass or remove an excessively disruptive safeguard.
func TestPrepareConfigUpdateAllowsLegitimateEdits(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		stored   map[string]any
		incoming map[string]any
	}{
		{
			name:     "Rename only, with unchanged configuration resubmitted",
			kind:     KindWebhook,
			stored:   map[string]any{"url": "https://legit.example.com/hook", "headers": map[string]any{"Authorization": "Bearer REAL"}},
			incoming: map[string]any{"url": MaskedValue("https://legit.example.com/hook")},
		},
		{
			name:     "Change request method only, preserving address and credentials",
			kind:     KindWebhook,
			stored:   map[string]any{"url": "https://legit.example.com/hook", "method": "POST"},
			incoming: map[string]any{"method": "PUT"},
		},
		{
			name:     "Change address and supply new credentials together",
			kind:     KindWebhook,
			stored:   map[string]any{"url": "https://old.example.com/hook", "headers": map[string]any{"Authorization": "Bearer OLD"}},
			incoming: map[string]any{"url": "https://new.example.com/hook", "headers": map[string]any{"Authorization": "Bearer NEW"}},
		},
		{
			name:     "Change address and explicitly clear unneeded credentials",
			kind:     KindWebhook,
			stored:   map[string]any{"url": "https://old.example.com/hook", "headers": map[string]any{"Authorization": "Bearer OLD"}},
			incoming: map[string]any{"url": "https://new.example.com/hook", "headers": ""},
		},
		{
			name:     "Telegram changes chat_id, which is not the destination",
			kind:     KindTelegram,
			stored:   map[string]any{"bot_token": "t", "chat_id": "1", "base_url": "https://api.telegram.org"},
			incoming: map[string]any{"chat_id": "-100200"},
		},
		{
			name:     "Email changes recipient, which is not the destination",
			kind:     KindEmail,
			stored:   map[string]any{"host": "smtp.corp.com", "port": 587, "password": "PW", "from": "a@b.c", "to": []any{"x@y.z"}},
			incoming: map[string]any{"to": []any{"new@y.z"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged, err := PrepareConfigUpdate(tc.kind, tc.stored, tc.incoming)
			if err != nil {
				t.Fatalf("Legitimate edit was rejected: %v", err)
			}
			if merged == nil {
				t.Fatal("Expected merged configuration")
			}
		})
	}
}

// TestPrepareConfigUpdatePortTypeTolerance covers numerically equal ports with
// different types, such as int and JSON float64. Direct equality could mistake an
// unchanged port for a new destination and demand passwords after a simple rename,
// creating false alarms that erode trust in the safeguard.
func TestPrepareConfigUpdatePortTypeTolerance(t *testing.T) {
	stored := map[string]any{"host": "smtp.corp.com", "port": float64(587), "password": "PW"}
	// Submit the same port as an int.
	if _, err := PrepareConfigUpdate(KindEmail, stored, map[string]any{"port": 587}); err != nil {
		t.Fatalf("Equal port values with different types must not count as an address change: %v", err)
	}
	// A real port change must still be blocked.
	if _, err := PrepareConfigUpdate(KindEmail, stored, map[string]any{"port": 25}); err == nil {
		t.Fatal("Port changes must be rejected")
	}
}

// TestPrepareConfigUpdateSurvivesRepeatedSaveWithBlankDestination covers optional
// destination fields: an empty Telegram base_url uses the official API.
//
// Previously this could make every save after the first fail:
//   - Creation stored base_url:"" directly, without MergeConfig.
//   - The first save treated the empty string as a clear operation and deleted the key.
//   - The second save compared incoming "" with a missing stored key as a changed address.
//   - The masked bot_token then caused a 400 requiring new destination credentials.
//
// An unchanged channel became impossible to save without pasting its Bot Token again.
func TestPrepareConfigUpdateSurvivesRepeatedSaveWithBlankDestination(t *testing.T) {
	stored := map[string]any{"bot_token": "123:ABC", "chat_id": "-100", "base_url": ""}

	// Frontend buildConfig() submits every declared field: masks for credentials and
	// empty strings for blank text inputs. Reproduce that full output, not just changed keys.
	submit := func() map[string]any {
		return map[string]any{
			"bot_token": MaskedValue("123:ABC"),
			"chat_id":   "-100",
			"base_url":  "",
		}
	}

	// First save: rename the channel and resubmit its configuration unchanged.
	merged, err := PrepareConfigUpdate(KindTelegram, stored, submit())
	if err != nil {
		t.Fatalf("First save was rejected: %v", err)
	}
	if _, ok := merged["base_url"]; ok {
		t.Fatal("Precondition changed: MergeConfig must remove an empty string; this case specifically covers the step after the key disappears")
	}

	// Second save: submit identical content with no user changes.
	merged2, err := PrepareConfigUpdate(KindTelegram, merged, submit())
	if err != nil {
		t.Fatalf("Second save was rejected despite no user changes: %v", err)
	}
	// Third save verifies consistently successful saves.
	if _, err := PrepareConfigUpdate(KindTelegram, merged2, submit()); err != nil {
		t.Fatalf("Third save was rejected: %v", err)
	}
	// Credentials must survive every save and must not be cleared by empty-string handling.
	if got := merged2["bot_token"]; got != "123:ABC" {
		t.Fatalf("Bot Token must retain its original value, got %v", got)
	}
}

// TestPrepareConfigUpdateStillGuardsBlankDestinationChanges pairs with the prior
// test. Treating empty and missing values equally must not allow real URL changes.
// Both directions can disclose credentials: Telegram embeds Bot Token in the path,
// so changing base_url sends it to a new destination.
func TestPrepareConfigUpdateStillGuardsBlankDestinationChanges(t *testing.T) {
	// Direction one: empty, meaning the official API, to a custom URL.
	official := map[string]any{"bot_token": "123:ABC", "chat_id": "-100"}
	if _, err := PrepareConfigUpdate(KindTelegram, official, map[string]any{
		"bot_token": MaskedValue("123:ABC"),
		"base_url":  "https://tg-proxy.attacker.tld",
	}); err == nil {
		t.Fatal("Switching from the official URL to a custom URL must require a newly supplied Token")
	}

	// Direction two: clearing a custom URL restores the official API and also changes destination.
	proxied := map[string]any{"bot_token": "123:ABC", "base_url": "https://proxy.internal/bot"}
	if _, err := PrepareConfigUpdate(KindTelegram, proxied, map[string]any{
		"bot_token": MaskedValue("123:ABC"),
		"base_url":  "",
	}); err == nil {
		t.Fatal("Clearing a custom URL to restore the official API is also an address change and must require a newly supplied Token")
	}
}

func TestDestinationKeysDeclaredForEveryKind(t *testing.T) {
	// Like SecretKeys, missing destination declarations leave a channel outside PrepareConfigUpdate protection.
	for kind, ch := range registry {
		if len(ch.DestinationKeys()) == 0 {
			t.Errorf("Channel %s declares no destination keys, so credential disclosure protection cannot cover address changes", kind)
		}
		if len(ch.SecretKeys()) == 0 {
			t.Errorf("Channel %s declares no credential keys", kind)
		}
	}
}

func TestSecretKeysDeclaredForEveryKind(t *testing.T) {
	// The compiler requires SecretKeys; also verify every channel declares credentials.
	// An empty slice would return its credentials to the browser in plaintext.
	expect := map[string]bool{
		KindDingTalk: true, KindFeishu: true, KindWeCom: true,
		KindWebhook: true, KindTelegram: true, KindEmail: true,
	}
	for kind, ch := range registry {
		if !expect[kind] {
			t.Errorf("Channel %s has no registered masking expectation in this test", kind)
			continue
		}
		if len(ch.SecretKeys()) == 0 {
			t.Errorf("Channel %s declares no credential fields, so its configuration would be returned in plaintext", kind)
		}
	}
}

// TestPrepareConfigUpdateRejectsMaskedInContainer covers mask sentinels nested
// in non-string values, such as the webhook.headers object. MergeConfig recognizes
// only strings with the prefix, so a nested literal "__masked__" could otherwise
// be stored as a real header and silently break authentication.
func TestPrepareConfigUpdateRejectsMaskedInContainer(t *testing.T) {
	stored := map[string]any{
		"url":     "https://legit.example.com/hook",
		"headers": map[string]any{"Authorization": "Bearer REAL"},
	}
	// Mask sentinel nested in an object.
	incoming := map[string]any{
		"headers": map[string]any{"Authorization": MaskedPrefix},
	}
	if _, err := PrepareConfigUpdate(KindWebhook, stored, incoming); err == nil {
		t.Fatal("A mask sentinel nested in a struct must be rejected to prevent storing it literally")
	}
	// An object containing actual new values remains valid.
	ok := map[string]any{"headers": map[string]any{"Authorization": "Bearer NEW"}}
	if _, err := PrepareConfigUpdate(KindWebhook, stored, ok); err != nil {
		t.Fatalf("A normal submission of new headers must not be rejected: %v", err)
	}
}
