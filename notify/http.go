package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// allowLocalTargets decides whether delivering a message to a loopback /
// link-local address is allowed.
//
// Denied by default. These address ranges aren't where an IM bot or public mail
// server would be, and what they can reach is sensitive: the management port of
// another service on the same host, and the cloud metadata endpoint
// (169.254.169.254, which can read out instance credentials). Delivery addresses
// are set by an admin, but a management session borrowed via XSS/CSRF, or a
// second person sharing the same JWT, could read the response content back by
// editing the config — doJSON writes the first 200 bytes of a 4xx/5xx response
// body into last_error, and the delivery history endpoint echoes it, which is a
// half-blind read primitive.
//
// But a "local SMTP relay" (postfix on 127.0.0.1:25) is a common self-hosted mail
// configuration, and a blanket block would get people stuck. So leave an explicit
// escape hatch rather than a hardcoded allow: set ARTEX_NOTIFY_ALLOW_LOCAL=1 to
// permit it.
//
// It's exported as AllowLocalTargetsEnv so tests can explicitly turn it on — this
// package's and the server package's cases make heavy use of httptest fake
// receivers on 127.0.0.1, which the guard would otherwise all block.
const AllowLocalTargetsEnv = "ARTEX_NOTIFY_ALLOW_LOCAL"

func allowLocalTargets() bool {
	v := strings.TrimSpace(os.Getenv(AllowLocalTargetsEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// isBlockedDialIP reports whether the target IP falls in an address range that's
// "not allowed for delivery by default".
//
// It rejects only loopback, link-local (including cloud metadata
// 169.254.169.254), unspecified, and multicast. It does **not** reject RFC1918
// private networks: an internal self-hosted Mattermost / SMTP relay is a very
// common legitimate use, and blocking those too would make the feature directly
// unusable in real environments. This tradeoff is deliberate — the defense must
// block genuinely sensitive targets without taking down normal deployments.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// IPv4-mapped IPv6 (::ffff:127.0.0.1) must be converted back to IPv4 before
	// judging, or it bypasses the check.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// blockInternalDial is the Control hook of the http.Transport dialer, checking
// the target address **at connection time**.
//
// Why it's set at the dial stage rather than only validating when the config is
// saved: this is the real point of enforcement. It covers both ways of bypassing
// config validation — DNS rebinding (resolving to a public IP at validation time,
// an internal one at actual connection time) and redirects (although we already
// reject cross-host jumps, a same-host jump can still point the path elsewhere).
func blockInternalDial(_, address string, _ syscall.RawConn) error {
	if allowLocalTargets() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("cannot resolve target address %q", host)
	}
	if isBlockedDialIP(ip) {
		return fmt.Errorf("refusing to deliver to loopback/link-local address %s (to deliver to a local service on purpose, set %s=1)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

// notifyTransport adds just one dial guard on top of the default Transport.
// Clone preserves all the default tuning (connection pool, HTTP/2, timeouts,
// proxy, etc.), avoiding changes to other behavior for the sake of adding one
// check.
var notifyTransport = func() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	clone := t.Clone()
	clone.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: blockInternalDial}).DialContext
	return clone
}()

// httpClient is the client shared by all channel deliveries.
//
// It deliberately does **not** reuse the project's global egress proxy (the
// server side's GlobalProxy): that proxy is for pentest target traffic, often an
// unstable tunnel, and notification availability shouldn't be held hostage to the
// jitter of the target network. IM pushes connect directly. The timeout is 15
// seconds — a peer slower than that is effectively in a failed state already.
//
// Reject cross-host redirects: this feature's delivery addresses are all a
// "single fixed endpoint" shape and normally don't redirect to another host; and
// these channels' credentials (DingTalk's access_token, WeCom's key, Telegram's
// bot token) are **right in the URL**, so following a cross-host jump hands the
// credential to the redirect target. A same-host jump (such as appending a
// trailing slash) is still allowed.
var httpClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: notifyTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("refusing cross-host redirect (%s → %s)", via[0].URL.Host, req.URL.Host)
		}
		return nil
	},
}

// respBodyLimit caps the size of the response body read. A misbehaving peer may
// spew back huge content, while we only need the error code and a short error
// description to show in the delivery history.
const respBodyLimit = 8 << 10

// doJSON sends one request and returns the response body (already length-capped).
//
// When payload is nil it sends an empty body (for GET or platforms that don't
// require a body). The key/values in headers are attached as-is, for the generic
// webhook's custom headers.
//
// Error classification is this function's core responsibility: network-layer
// failures and 5xx/408/429 are "retryable", the rest of the 4xx are "permanent"
// — retrying a 403 just prints the same error in the log three times.
func doJSON(ctx context.Context, method, url string, headers map[string]string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			// A serialization failure is a local bug (wrong config field type) that a
			// retry won't improve.
			return nil, Permanent(fmt.Errorf("failed to build request body: %w", err))
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// Invalid URL — most likely the user mistyped the address, a permanent
		// failure. Here too err must not be passed through: url.Parse's error text
		// contains the full URL.
		return nil, Permanent(fmt.Errorf("invalid request URL: %s", redactRequestTarget(url)))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Connection refused, DNS failure, timeout — mostly transient faults, handed
		// to backoff retry.
		//
		// The error text must be redacted before being passed outward. Why:
		// http.Client.Do returns a *url.Error whose Error() is `Op "full URL":
		// underlying error`, and these channels' credentials are **right in the URL**
		// (DingTalk access_token, WeCom key, Feishu hook id, Telegram /bot<token>/).
		// Without redaction, the credential flows along this error string to four
		// places: notification_deliveries' last_error (persisted in cleartext), the
		// delivery history endpoint's response (**bypassing the channel config's
		// masking**), the server logs, and the 502 text the test-send endpoint
		// returns to the frontend.
		return nil, fmt.Errorf("request failed: %s", redactTransportError(err))
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if readErr != nil {
		return nil, fmt.Errorf("failed to read response: %w", readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	// 429 (rate limited) and 408 (timeout) are worth retrying; the rest of the 4xx
	// are config or permission problems where a retry is pointless.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
		return nil, fmt.Errorf("peer rate limited or timed out (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("peer service error (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	return nil, Permanent(fmt.Errorf("peer rejected the request (HTTP %d): %s", resp.StatusCode, snippet(raw)))
}

// snippet compresses the response body to a short single line for error messages.
// The response may carry newlines and lots of whitespace, and dropping it
// straight into last_error would break the delivery history page's layout.
func snippet(raw []byte) string {
	return OneLine(string(raw), 200)
}

// redactRequestTarget compresses a delivery address to "scheme://host/…" for
// error messages.
//
// This is the package's only address-redaction policy, and it is deliberately
// **blunt enough**: everything but the scheme and host is discarded. The reason
// is that there's no "general and safe" way to tell which part of the URL is the
// credential:
//
//	DingTalk  credential in query        /robot/send?access_token=xxx
//	WeCom     credential in query        /cgi-bin/webhook/send?key=xxx
//	Feishu    credential in **last path segment** /open-apis/bot/v2/hook/<hook_id>
//	Telegram  credential in **mid path**  /bot<token>/sendMessage
//
// Trying to "keep only the useful part" would require per-channel patching, and
// missing any one of them is a credential leak. Keeping the host is already
// enough for troubleshooting (DNS not resolving, can't connect, wrong cert are
// all locatable), and which bot it is can be recognized from the masked
// trailing digits in the channel config.
//
// On a parse failure it returns a fixed placeholder — the original string is
// never echoed out.
func redactRequestTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(unparseable address)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// redactTransportError strips the address from a transport-layer error, keeping
// only the underlying cause.
//
// A *url.Error's structure is {Op, URL, Err}, and Error() prints the URL along
// with it. Here we explicitly take the Err field, bypassing its Error() — more
// reliable than string replacement after the fact, because replacement would have
// to correctly handle all the URL-encoded/escaped variants and easily misses one.
func redactTransportError(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		host := ""
		if u, parseErr := url.Parse(uerr.URL); parseErr == nil {
			host = u.Host
		}
		if uerr.Err != nil {
			return fmt.Sprintf("%s %s: %s", uerr.Op, host, uerr.Err)
		}
		return fmt.Sprintf("%s %s: unknown error", uerr.Op, host)
	}
	// A non-*url.Error (such as the error returned by the redirect policy) may also
	// carry an address, so route it through redaction uniformly.
	return redactURLsInText(err.Error())
}

// redactURLsInText replaces http(s) addresses appearing in a piece of text with
// their redacted form.
//
// Used as a backstop for errors with no structured field available (redirect
// policy errors, third-party libraries' custom errors). It recognizes only an
// http/https prefix and splits on whitespace and quotes — an address won't
// contain either kind of character.
func redactURLsInText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
			end := len(rest)
			if j := strings.IndexAny(rest, " \t\n\"'"); j >= 0 {
				end = j
			}
			b.WriteString(redactRequestTarget(rest[:end]))
			i += end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
