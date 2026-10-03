package notify

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// This file covers two related hardenings:
//   1. a delivery address must not use the server as a jump box into the internal
//      network / cloud metadata (SSRF)
//   2. address-validation error messages must not carry the credential in the
//      address out
//
// About the test environment: this package makes heavy use of httptest fake
// receivers on 127.0.0.1, which the guard blocks by default. So TestMain turns on
// AllowLocalTargetsEnv globally, and each SSRF case below explicitly clears it to
// assert the **deny-by-default** behavior.

func TestMain(m *testing.M) {
	// Let regular cases reach the local fake receivers; SSRF cases clear it
	// temporarily themselves.
	_ = os.Setenv(AllowLocalTargetsEnv, "1")
	os.Exit(m.Run())
}

// TestDialGuardRejectsLoopbackByDefault is the SSRF defense's core assertion:
// under the default config, delivery to a loopback address must be refused at the
// **connection layer**.
func TestDialGuardRejectsLoopbackByDefault(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "") // turn off the escape hatch = default behavior
	_, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}})
	if err == nil {
		t.Fatal("by default, delivery to a loopback address should not be allowed")
	}
	if hit {
		t.Fatal("the request reached the local service — the guard didn't take effect")
	}
	// The error should guide the user on how to open it up (a local SMTP relay is a
	// legitimate config).
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("the refusal should say how to explicitly open it up: %v", err)
	}
}

// TestDialGuardAllowsLoopbackWhenOptedIn is the reverse case: once explicitly
// turned on it must work, or legitimate deployments like local postfix / internal
// relays get taken down wholesale.
func TestDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}}); err != nil {
		t.Fatalf("should be deliverable once explicitly opened up: %v", err)
	}
}

func TestIsBlockedDialIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1",
		"169.254.169.254", // the cloud metadata endpoint — the main reason this function exists
		"169.254.1.1", "fe80::1",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", // the IPv4-mapped form must be converted back before judging, or it's a bypass
		"",
	}
	for _, s := range blocked {
		if !isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be rejected", s)
		}
	}
	// RFC1918 private networks are **deliberately allowed**: an internal self-hosted
	// Mattermost / SMTP relay is a common legitimate use. This assertion pins that
	// tradeoff — if someone later casually adds a private-network check, this fails
	// and forces a conscious decision (rather than silently killing a batch of
	// deployments).
	allowed := []string{"10.0.0.5", "172.16.3.4", "192.168.1.10", "8.8.8.8", "2606:4700::1111"}
	for _, s := range allowed {
		if isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed (private networks are a common legitimate delivery target)", s)
		}
	}
}

// TestValidateHTTPURLRejectsLiteralPrivateTargets covers the config-stage early
// feedback: a literal IP should be rejected at save time, not only when the first
// delivery fails.
func TestValidateHTTPURLRejectsLiteralPrivateTargets(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "")
	for _, raw := range []string{
		"http://127.0.0.1:8080/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]:8080/hook",
	} {
		if err := validateHTTPURL(raw); err == nil {
			t.Errorf("%s should be rejected at the config stage", raw)
		}
	}
	// Public and private-network addresses pass as usual (private networks are left
	// to the dial stage, which doesn't block them here).
	for _, raw := range []string{"https://oapi.dingtalk.com/robot/send", "http://10.0.0.9/hook"} {
		if err := validateHTTPURL(raw); err != nil {
			t.Errorf("%s should pass validation: %v", raw, err)
		}
	}
}

// TestValidateHTTPURLErrorNeverLeaksCredentials is the branch the audit pointed
// out I missed last round.
//
// When url.Parse **fails** it returns a *url.Error whose Error() contains the full
// original address. Last round I only redacted http.Client.Do's returned error
// and missed this one; and the "permanent failure path" cases I added then
// (file://, gopher://, ftp://) actually all parse successfully with url.Parse and
// take the scheme branch, so all-green didn't prove this path safe — a false
// guarantee.
func TestValidateHTTPURLErrorNeverLeaksCredentials(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/%zz?access_token=" + leakProbeToken,         // invalid percent-escape
		"https://a.example.com:port/x?access_token=" + leakProbeToken, // non-numeric port
		"http://[::1?access_token=" + leakProbeToken,                  // unmatched bracket
	}
	for _, raw := range cases {
		// First confirm the input **does** make url.Parse fail. Without this step, a
		// case might unknowingly take a different branch (that's how last round's
		// false guarantee happened).
		if _, err := url.Parse(raw); err == nil {
			t.Errorf("%q was supposed to fail to parse, otherwise this case doesn't cover the target branch", raw)
			continue
		}
		err := validateHTTPURL(raw)
		if err == nil {
			t.Errorf("%q should fail validation", raw)
			continue
		}
		assertNoSecret(t, err.Error(), leakProbeToken)
	}
	// Confirm the channel-layer wrapping doesn't carry the address out either.
	t.Setenv(AllowLocalTargetsEnv, "")
	err := (dingTalkChannel{}).Validate(map[string]any{"webhook": cases[0]})
	if err == nil {
		t.Fatal("an invalid address should fail validation")
	}
	assertNoSecret(t, err.Error(), leakProbeToken)
}

// TestEmailDialGuardRejectsLoopbackByDefault covers the SMTP channel's dial guard.
//
// The email channel used to use a bare net.Dialer, the single hole in the whole
// SSRF defense: a host of 169.254.169.254 or 127.0.0.1 could be connected to
// directly, and when smtp.NewClient's handshake fails it wraps the peer's
// returned line into the error, echoed by the delivery history endpoint via
// last_error — exactly the half-blind read primitive the other channels already
// closed; the "connection refused vs. timeout" timing difference could also be
// used to probe ports.
//
// This package's TestMain turns on AllowLocalTargetsEnv globally (many cases use
// httptest fake receivers on 127.0.0.1), so this case must clear it itself —
// otherwise it would pass whether the guard existed or not, which is exactly why
// the hole went undetected by any test originally.
func TestEmailDialGuardRejectsLoopbackByDefault(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "") // turn off the escape hatch = default behavior
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Fatal("by default, delivering email to a loopback address should not be allowed")
	}
	// The connection should never be established: the guard blocks it in the
	// Control hook, so EHLO is never sent.
	if f.sawCommand("EHLO") || f.sawCommand("HELO") {
		t.Fatal("the SMTP session was established — the guard didn't take effect")
	}
	// The error should guide the user on how to open it up (a local postfix relay
	// is a legitimate config).
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("the refusal should say how to explicitly open it up: %v", err)
	}
}

// TestEmailDialGuardAllowsLoopbackWhenOptedIn is the paired reverse case: once
// explicitly turned on it must deliver normally. An internal self-hosted SMTP /
// local relay is a very common deployment, and the guard can't be blanket.
func TestEmailDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("local SMTP should be deliverable once explicitly opened up: %v", err)
	}
	if !f.sawCommand("EHLO") {
		t.Fatal("no EHLO seen — the session wasn't really established")
	}
}
