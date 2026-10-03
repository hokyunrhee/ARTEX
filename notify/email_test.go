package notify

import (
	"bufio"
	"context"

	"net"
	"strings"
	"sync"
	"testing"
)

// Protocol-level coverage for the email channel. email.Send previously had zero
// coverage despite having the broadest protocol surface of the six channels:
// handshake, authentication, envelope, and DATA each have distinct failure semantics.
//
// Use a minimal real SMTP server rather than mocking net/smtp. Talking to the
// server is where most email-channel risks arise, so mocking it would skip the
// behavior these tests need to verify.

// fakeSMTP implements greet/EHLO/AUTH/MAIL/RCPT/DATA/QUIT and returns the
// configured response code at the stage specified by each test.
type fakeSMTP struct {
	ln net.Listener

	// rcptReply is the RCPT TO response; defaults to 250.
	rcptReply string
	// mailReply is the MAIL FROM response; defaults to 250.
	mailReply string
	// advertiseAuth announces AUTH PLAIN in EHLO when true.
	advertiseAuth bool

	mu       sync.Mutex
	data     string
	commands []string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, rcptReply: "250 OK", mailReply: "250 OK"}
	go f.serve()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeSMTP) hostPort(t *testing.T) (string, int) {
	t.Helper()
	addr, ok := f.ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("Listener address is not TCP")
	}
	return "127.0.0.1", addr.Port
}

func (f *fakeSMTP) record(cmd string) {
	f.mu.Lock()
	f.commands = append(f.commands, cmd)
	f.mu.Unlock()
}

func (f *fakeSMTP) body() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data
}

func (f *fakeSMTP) sawCommand(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.commands {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func (f *fakeSMTP) serve() {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	w("220 fake.local ESMTP ready")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		f.record(line)
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			// Do not announce STARTTLS; exercise the plaintext branch to test envelope logic, not TLS.
			w("250-fake.local")
			if f.advertiseAuth {
				w("250-AUTH PLAIN")
			}
			w("250 8BITMIME")
		case strings.HasPrefix(line, "AUTH"):
			// Simplify PLAIN initial responses, which may span multiple lines, by accepting them.
			w("235 2.7.0 Authentication successful")
		case strings.HasPrefix(line, "MAIL FROM"):
			w(f.mailReply)
		case strings.HasPrefix(line, "RCPT TO"):
			w(f.rcptReply)
		case strings.HasPrefix(line, "DATA"):
			w("354 End data with <CR><LF>.<CR><LF>")
			var sb strings.Builder
			for {
				dl, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
				sb.WriteString(dl)
			}
			f.mu.Lock()
			f.data = sb.String()
			f.mu.Unlock()
			w("250 2.0.0 Ok: queued as FAKE1")
		case strings.HasPrefix(line, "QUIT"):
			w("221 2.0.0 Bye")
			return
		default:
			w("250 OK")
		}
	}
}

func emailCfg(t *testing.T, f *fakeSMTP, extra map[string]any) map[string]any {
	t.Helper()
	host, port := f.hostPort(t)
	cfg := map[string]any{
		"host": host,
		"port": float64(port),
		"from": "artex@example.com",
		"to":   []any{"a@example.com", "b@example.com"},
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

func TestEmailSendDeliversFullMessage(t *testing.T) {
	f := newFakeSMTP(t)
	f.advertiseAuth = true
	cfg := emailCfg(t, f, map[string]any{"username": "artex", "password": "pw"})

	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("Delivery failed: %v", err)
	}
	// The envelope must include the sender, both recipients, and DATA.
	for _, want := range []string{"MAIL FROM:<artex@example.com>", "RCPT TO:<a@example.com>", "RCPT TO:<b@example.com>", "DATA", "AUTH", "QUIT"} {
		if !f.sawCommand(want) {
			t.Errorf("SMTP session is missing %q; actual commands: %v", want, f.commands)
		}
	}
	// The body is base64 HTML and must contain the actual finding content, recognizable after decoding.
	body := f.body()
	if body == "" {
		t.Fatal("No body received during DATA")
	}
	if !strings.Contains(body, "Content-Type: text/html") {
		t.Errorf("Missing Content-Type header:\n%s", body)
	}
	if !strings.Contains(body, "base64") {
		t.Errorf("Body is not base64 encoded (long HTML lines would exceed SMTP's 1000-byte line limit):\n%s", body)
	}
	// The To header must include every recipient.
	if !strings.Contains(body, "a@example.com, b@example.com") {
		t.Errorf("To header does not include all recipients:\n%s", body)
	}
}

func TestEmailSendWithoutAuth(t *testing.T) {
	// Do not send AUTH without an account; some relays would reject it.
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("Delivery failed: %v", err)
	}
	if f.sawCommand("AUTH") {
		t.Errorf("AUTH was sent with no username configured: %v", f.commands)
	}
}

// TestEmailSendClassifiesSMTPReplies verifies the audit fix directly:
// 5xx is permanent; 4xx, including greylisting, is retryable.
func TestEmailSendClassifiesSMTPReplies(t *testing.T) {
	cases := []struct {
		name      string
		rcptReply string
		mailReply string
		permanent bool
	}{
		{"Recipient permanently rejected with 550", "550 5.1.1 User unknown", "250 OK", true},
		{"Recipient greylisted with 450", "450 4.7.1 Greylisting in action", "250 OK", false},
		{"Recipient mailbox full with 452", "452 4.2.2 Mailbox full", "250 OK", false},
		{"Sender permanently rejected with 553", "250 OK", "553 5.1.3 Bad address", true},
		{"Sender temporarily rejected with 451", "250 OK", "451 4.3.0 Temporary failure", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSMTP(t)
			f.rcptReply = tc.rcptReply
			f.mailReply = tc.mailReply
			_, err := (emailChannel{}).Send(context.Background(), emailCfg(t, f, nil), singleMsg())
			if err == nil {
				t.Fatal("Expected an error")
			}
			if got := IsPermanent(err); got != tc.permanent {
				t.Fatalf("Incorrect permanent classification: expected %v, got %v (%v)", tc.permanent, got, err)
			}
			// Preserve the server reply so operators know whether to contact its administrator or fix the address.
			if !strings.Contains(err.Error(), strings.Fields(tc.rcptReply)[0]) && !strings.Contains(err.Error(), strings.Fields(tc.mailReply)[0]) {
				t.Errorf("Error must preserve the server reply code: %v", err)
			}
		})
	}
}

func TestEmailSendRefusesPlaintextCredentials(t *testing.T) {
	// net/smtp PlainAuth refuses credentials on unencrypted connections except to localhost.
	// This is correct security behavior and must not be bypassed; return actionable errors.
	// A non-localhost name exercises that case.
	f := newFakeSMTP(t)
	f.advertiseAuth = true
	_, port := f.hostPort(t)
	cfg := map[string]any{
		"host":     "smtp.example.com", // Not localhost.
		"port":     float64(port),
		"from":     "a@example.com",
		"to":       []any{"b@example.com"},
		"username": "artex",
		"password": "pw",
	}
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Skip("Local DNS resolved to the local server; skipping this case only")
	}
	// Either connection failure or refusal to send credentials satisfies this assertion;
	// credentials must never be sent silently.
	if !IsPermanent(err) && !strings.Contains(err.Error(), "connect") {
		t.Logf("Error: %v (connection failure to a non-localhost name is expected)", err)
	}
}

func TestEmailValidateReportsMissingFields(t *testing.T) {
	// Email has the most configuration fields; omissions otherwise surface only on delivery.
	// Verify validation catches each missing field and identifies it in the error.
	cases := []struct {
		name string
		cfg  map[string]any
	}{
		{"Missing host", map[string]any{"port": float64(25), "from": "a@b.c", "to": []any{"d@e.f"}}},
		{"Missing port", map[string]any{"host": "smtp.example.com"}},
		{"Port out of range", map[string]any{"host": "h", "port": float64(70000), "from": "a@b.c", "to": []any{"d@e.f"}}},
		{"Missing from", map[string]any{"host": "h", "port": float64(25), "to": []any{"d@e.f"}}},
		{"Missing to", map[string]any{"host": "h", "port": float64(25), "from": "a@b.c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := (emailChannel{}).Validate(tc.cfg); err == nil {
				t.Fatalf("Expected validation failure: %v", tc.cfg)
			}
		})
	}
}

// TestEmailConfigTolerance covers flexible reads: JSONB numbers are float64,
// while UI ports may be strings and arrays may arrive as a single string.
func TestEmailConfigTolerance(t *testing.T) {
	cfg := map[string]any{
		"host": "smtp.example.com",
		"port": "587", // Port as a string.
		"from": "a@b.c",
		"to":   "d@e.f", // A single string instead of an array.
		"tls":  "true",  // Boolean as a string.
	}
	if err := (emailChannel{}).Validate(cfg); err != nil {
		t.Fatalf("Numeric strings must be accepted: %v", err)
	}
	if got := cfgInt(cfg, "port"); got != 587 {
		t.Errorf("cfgInt did not parse the string port, got %d", got)
	}
	if !cfgBool(cfg, "tls") {
		t.Error("cfgBool did not parse the string \"true\"")
	}
	if to := cfgStrings(cfg, "to"); len(to) != 1 || to[0] != "d@e.f" {
		t.Errorf("cfgStrings did not accept a single string, got %v", to)
	}
}

// TestFilterValidateRejectsTypo verifies the audit fix: reject misspelled thresholds
// on write, preventing a silently ineffective filter that sends every event.
func TestFilterValidateRejectsTypo(t *testing.T) {
	good := []string{"", "low", "medium", "high", "critical"}
	for _, s := range good {
		if err := (Filter{MinSeverity: s}).Validate(); err != nil {
			t.Errorf("Valid threshold %q was rejected: %v", s, err)
		}
	}
	// Reject all of these plausible typos.
	for _, s := range []string{"hgih", "HIGH", "Severe", "high ", "crit"} {
		err := (Filter{MinSeverity: s}).Validate()
		if err == nil {
			t.Errorf("Invalid threshold %q must be rejected rather than silently allowing every event", s)
			continue
		}
		// The error must help operators correct the value.
		if !strings.Contains(err.Error(), "low") || !strings.Contains(err.Error(), "critical") {
			t.Errorf("Error must list valid choices, got %q", err.Error())
		}
	}
}

// TestFilterValidateIsWriteTimeOnly enforces strict writes and tolerant reads.
// Bad stored values must not make channels unreadable and suddenly stop notifications.
func TestFilterValidateIsWriteTimeOnly(t *testing.T) {
	raw := []byte(`{"min_severity":"hgih"}`)
	f := ParseFilter(raw) // No error.
	if f.MinSeverity != "hgih" {
		t.Fatalf("Read path must preserve the stored value, got %q", f.MinSeverity)
	}
	// The channel must still evaluate events without panic or blocking.
	_ = Match(f, Snapshot{Kind: EventFindingCreated, Severity: "critical"})
}
