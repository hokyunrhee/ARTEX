package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// emailDialTimeout bounds connection setup; emailSessionTimeout bounds the whole
// SMTP session. net/smtp provides no timeouts. Without both, a stalled peer could
// block a delivery goroutine forever, stopping the entire serial dispatcher.
const (
	emailDialTimeout    = 10 * time.Second
	emailSessionTimeout = 45 * time.Second
)

// emailChannel implements SMTP email delivery.
type emailChannel struct{}

func (emailChannel) Kind() string { return KindEmail }

// Email has no platform rate limit, but use a generous default to avoid flooding inboxes.
func (emailChannel) DefaultRatePerMin() int { return 60 }

// Mask only passwords. SMTP hosts, usernames, and recipients are not secrets here;
// masking them would make editing unnecessarily difficult.
func (emailChannel) SecretKeys() []string { return []string{"password"} }

// host/port select the server receiving the password, and tls controls encryption.
// Any change requires an explicit password choice, including when disabling TLS.
func (emailChannel) DestinationKeys() []string { return []string{"host", "port", "tls"} }

func (emailChannel) Validate(cfg map[string]any) error {
	if cfgString(cfg, "host") == "" {
		return errors.New("Missing SMTP server address")
	}
	port := cfgInt(cfg, "port")
	if port <= 0 || port > 65535 {
		return errors.New("Invalid SMTP port (must be 1-65535)")
	}
	if cfgString(cfg, "from") == "" {
		return errors.New("Missing sender address")
	}
	if len(cfgStrings(cfg, "to")) == 0 {
		return errors.New("At least one recipient address is required")
	}
	return nil
}

func (c emailChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	host := cfgString(cfg, "host")
	port := cfgInt(cfg, "port")
	from := cfgString(cfg, "from")
	to := cfgStrings(cfg, "to")
	username := cfgString(cfg, "username")
	password := cfgString(cfg, "password")
	implicitTLS := cfgBool(cfg, "tls")

	msg, err := buildEmailMessage(from, to, m)
	if err != nil {
		return 0, Permanent(err)
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	client, err := emailDial(ctx, addr, host, implicitTLS)
	if err != nil {
		return 0, err
	}
	defer client.Close()

	// Upgrade with STARTTLS if supported. Never send credentials over a plaintext
	// session; see the authentication handling below.
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return 0, fmt.Errorf("STARTTLS failed: %w", err)
			}
		}
	}
	if username != "" {
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			// smtp.PlainAuth correctly rejects credentials on an unencrypted connection
			// (except localhost). Do not bypass this protection; explain it clearly so the
			// operator knows how to resolve an "unencrypted connection" error.
			if strings.Contains(err.Error(), "unencrypted connection") {
				return 0, Permanent(fmt.Errorf("Credentials were not sent: the connection is unencrypted. Enable TLS, use port 465 (implicit TLS), or select Enable TLS (%w)", err))
			}
			return 0, Permanent(fmt.Errorf("SMTP authentication failed: %w", err))
		}
	}
	if err := client.Mail(from); err != nil {
		return 0, smtpStageError(fmt.Sprintf("Sender %s rejected", from), err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return 0, smtpStageError(fmt.Sprintf("Recipient %s rejected", rcpt), err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return 0, fmt.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return 0, fmt.Errorf("Failed to write email body: %w", err)
	}
	if err := w.Close(); err != nil {
		return 0, fmt.Errorf("Failed to submit email: %w", err)
	}
	// A failed Quit does not undo server acceptance of the email; ignore that error.
	_ = client.Quit()
	// Email sends the entire HTML body without length truncation, so the whole batch is delivered.
	return len(m.Items), nil
}

// emailDial opens an SMTP connection.
//
// implicitTLS=true starts TLS immediately, as on port 465; false connects in
// plaintext on ports such as 25/587, then uses STARTTLS. Mixing these modes causes
// disconnection, for example a plaintext greeting on port 465.
//
// Set the session deadline while opening the connection: net/smtp hides its
// connection in an unexported field, so the deadline must already exist before
// handing it over. This also bounds a blocked handshake.
// Control uses the same blockInternalDial guard as HTTP channels. Without it, SMTP
// could reach 169.254.169.254 or 127.0.0.1; handshake errors include the peer reply
// and expose it through last_error and delivery history, creating a partial read
// primitive. Refused-versus-timeout timing could also reveal ports. Dial-time
// validation is the final enforcement point and covers DNS rebinding.
func emailDial(ctx context.Context, addr, host string, implicitTLS bool) (*smtp.Client, error) {
	d := &net.Dialer{Timeout: emailDialTimeout, Control: blockInternalDial}
	var conn net.Conn
	var err error
	if implicitTLS {
		conn, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to SMTP server: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(emailSessionTimeout))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMTP handshake failed: %w", err)
	}
	return client, nil
}

// smtpStageError classifies SMTP failures as retryable or permanent by reply code.
//
// SMTP distinguishes these cases:
//   - 4xx (450 greylisting, 451 local error, 452 insufficient storage) is temporary:
//     retry later. Greylisting commonly rejects the first delivery attempt.
//   - 5xx (550 unknown user, 553 invalid address) is permanent; retrying cannot help.
//
// Treating every failure as permanent would fail every notification on its first
// attempt against a greylisting server, where retries are particularly valuable.
// Read the first three digits of the error; if unavailable, permit retries rather
// than classify a potentially transient failure as permanent.
func smtpStageError(what string, err error) error {
	code := smtpReplyCode(err.Error())
	if code >= 500 && code < 600 {
		return Permanent(fmt.Errorf("%s: %w", what, err))
	}
	return fmt.Errorf("%s: %w", what, err)
}

// smtpReplyCode reads the leading three-digit reply code or returns zero. net/smtp
// does not export a code field, so parse text such as "450 4.7.1 ...".
func smtpReplyCode(text string) int {
	if len(text) < 3 {
		return 0
	}
	n, err := strconv.Atoi(text[:3])
	if err != nil {
		return 0
	}
	return n
}

// buildEmailMessage assembles an RFC 5322 email.
//
// Base64 prevents long HTML lines, especially digests, from exceeding SMTP's
// 1000-byte line limit. It also produces no lines starting with ".", avoiding
// SMTP dot-stuffing concerns.
func buildEmailMessage(from string, to []string, m Message) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	// Encode non-ASCII subjects with RFC 2047 so email clients display them correctly.
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", htmlTitle(m)))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	// Email has no hard body-length limit, so do not truncate it.
	b.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(htmlBody(m, 0)))
	// Wrap base64 at 76 characters as required by RFC 2045.
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return b.String(), nil
}
