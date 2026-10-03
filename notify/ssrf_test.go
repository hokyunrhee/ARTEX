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

// Two related safeguards:
//   1. Delivery destinations must not turn the server into an SSRF relay to local services or cloud metadata.
//   2. Address-validation errors must not disclose credentials in the address.
//
// Many tests use httptest receivers on 127.0.0.1, which the guard blocks by default.
// TestMain enables AllowLocalTargetsEnv for them; each SSRF test clears it explicitly
// to verify rejection by default.

func TestMain(m *testing.M) {
	// Allow ordinary tests to reach local fake receivers; SSRF tests temporarily clear this.
	_ = os.Setenv(AllowLocalTargetsEnv, "1")
	os.Exit(m.Run())
}

// TestDialGuardRejectsLoopbackByDefault verifies the core SSRF invariant:
// the connection layer must block loopback delivery in the default configuration.
func TestDialGuardRejectsLoopbackByDefault(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "") // Disable the opt-in override to restore default behavior.
	_, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}})
	if err == nil {
		t.Fatal("Loopback delivery must be blocked by default")
	}
	if hit {
		t.Fatal("Request reached the local service; the guard did not work")
	}
	// Explain how to opt in; a local SMTP relay is a legitimate configuration.
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("Rejection must explain how to opt in: %v", err)
	}
}

// TestDialGuardAllowsLoopbackWhenOptedIn checks the inverse: explicit opt-in must
// work, so legitimate local postfix and internal-relay deployments remain usable.
func TestDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}}); err != nil {
		t.Fatalf("Delivery must succeed after explicitly opting in: %v", err)
	}
}

func TestIsBlockedDialIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1",
		"169.254.169.254", // Cloud metadata endpoints are a primary reason for this function.
		"169.254.1.1", "fe80::1",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", // Unmap IPv4-mapped addresses before checking them to prevent bypasses.
		"",
	}
	for _, s := range blocked {
		if !isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s must be blocked", s)
		}
	}
	// RFC1918 private networks are deliberately allowed for legitimate self-hosted
	// Mattermost and SMTP relays. This assertion makes changes to that tradeoff explicit
	// instead of silently breaking existing deployments.
	allowed := []string{"10.0.0.5", "172.16.3.4", "192.168.1.10", "8.8.8.8", "2606:4700::1111"}
	for _, s := range allowed {
		if isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s must be allowed because private networks are legitimate delivery targets", s)
		}
	}
}

// TestValidateHTTPURLRejectsLiteralPrivateTargets checks early configuration
// feedback: reject blocked literal IPs on save, before the first failed delivery.
func TestValidateHTTPURLRejectsLiteralPrivateTargets(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "")
	for _, raw := range []string{
		"http://127.0.0.1:8080/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]:8080/hook",
	} {
		if err := validateHTTPURL(raw); err == nil {
			t.Errorf("%s must be rejected during configuration", raw)
		}
	}
	// Public and private addresses still validate; dialing also permits private networks.
	for _, raw := range []string{"https://oapi.dingtalk.com/robot/send", "http://10.0.0.9/hook"} {
		if err := validateHTTPURL(raw); err != nil {
			t.Errorf("%s must pass validation: %v", raw, err)
		}
	}
}

// TestValidateHTTPURLErrorNeverLeaksCredentials covers a previously missed branch.
//
// A failed url.Parse returns *url.Error containing the full original address.
// Earlier redaction covered only http.Client.Do errors. The permanent-path tests
// used file://, gopher://, and ftp://, all of which parse successfully and fail
// scheme validation, so passing them did not establish safety for parse failures.
func TestValidateHTTPURLErrorNeverLeaksCredentials(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/%zz?access_token=" + leakProbeToken,         // Invalid percent escape.
		"https://a.example.com:port/x?access_token=" + leakProbeToken, // Nonnumeric port.
		"http://[::1?access_token=" + leakProbeToken,                  // Unmatched brackets.
	}
	for _, raw := range cases {
		// First prove this input actually fails url.Parse. Otherwise the case could
		// silently exercise another branch and give false assurance, as it did previously.
		if _, err := url.Parse(raw); err == nil {
			t.Errorf("%q must fail parsing or this case does not cover the intended branch", raw)
			continue
		}
		err := validateHTTPURL(raw)
		if err == nil {
			t.Errorf("%q must fail validation", raw)
			continue
		}
		assertNoSecret(t, err.Error(), leakProbeToken)
	}
	// Verify channel-level wrapping does not reintroduce the address.
	t.Setenv(AllowLocalTargetsEnv, "")
	err := (dingTalkChannel{}).Validate(map[string]any{"webhook": cases[0]})
	if err == nil {
		t.Fatal("Invalid address must fail validation")
	}
	assertNoSecret(t, err.Error(), leakProbeToken)
}

// TestEmailDialGuardRejectsLoopbackByDefault covers the SMTP dial guard.
//
// Email previously used a bare net.Dialer, leaving the only SSRF gap: hosts such
// as 169.254.169.254 or 127.0.0.1 were reachable. Failed smtp.NewClient handshakes
// could expose a remote reply through last_error and delivery history, enabling
// the partial-read behavior blocked for other channels. Connection-refusal versus
// timeout timing also enabled port probing.
//
// TestMain enables AllowLocalTargetsEnv for local fake receivers. This case must
// clear it; otherwise it passes with or without the guard, explaining why the gap
// was originally missed.
func TestEmailDialGuardRejectsLoopbackByDefault(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "") // Disable the opt-in override to restore default behavior.
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Fatal("Email delivery to loopback must be blocked by default")
	}
	// No connection should be established: the Control hook blocks it before EHLO.
	if f.sawCommand("EHLO") || f.sawCommand("HELO") {
		t.Fatal("SMTP session was established; the guard did not work")
	}
	// Explain how to opt in; a local postfix relay is a legitimate configuration.
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("Rejection must explain how to opt in: %v", err)
	}
}

// TestEmailDialGuardAllowsLoopbackWhenOptedIn verifies explicit opt-in permits
// delivery. Self-hosted internal SMTP and local relays are common legitimate deployments.
func TestEmailDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("Local SMTP delivery must succeed after explicitly opting in: %v", err)
	}
	if !f.sawCommand("EHLO") {
		t.Fatal("No EHLO observed; the session was not established")
	}
}
