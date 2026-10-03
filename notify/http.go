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

// allowLocalTargets permits delivery to loopback and link-local addresses.
//
// It defaults to false. Public IM bots and mail servers do not use these ranges,
// but local administration ports and cloud metadata at 169.254.169.254 can expose
// sensitive data or instance credentials. Even administrator-configured URLs can
// be changed through a compromised XSS/CSRF session or a second person sharing the
// JWT. doJSON stores the first 200 response bytes from 4xx/5xx errors in last_error,
// which delivery history exposes, creating a partial read primitive.
//
// Local SMTP relays such as postfix on 127.0.0.1:25 are legitimate, common setups.
// Provide an explicit opt-in, ARTEX_NOTIFY_ALLOW_LOCAL=1, instead of an unconditional
// exception. AllowLocalTargetsEnv is exported so tests in this package and server
// can explicitly enable their httptest receivers on 127.0.0.1.
const AllowLocalTargetsEnv = "ARTEX_NOTIFY_ALLOW_LOCAL"

func allowLocalTargets() bool {
	v := strings.TrimSpace(os.Getenv(AllowLocalTargetsEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// isBlockedDialIP identifies address ranges blocked by default.
//
// Block only loopback, link-local (including cloud metadata at 169.254.169.254),
// unspecified, and multicast addresses. Do not block RFC1918 private networks:
// internal Mattermost servers and SMTP relays are legitimate deployments. This
// intentional boundary protects sensitive destinations without breaking those uses.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// Normalize IPv4-mapped IPv6 such as ::ffff:127.0.0.1 before checking to prevent bypass.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// blockInternalDial is the http.Transport dialer Control hook. It validates the
// destination when the connection is established, the final enforcement point.
//
// Save-time validation alone cannot cover DNS rebinding (public during validation,
// internal at connection time) or redirects. Cross-host redirects are blocked,
// but same-host redirects can still change the destination path.
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
		return fmt.Errorf("Cannot resolve target address %q", host)
	}
	if isBlockedDialIP(ip) {
		return fmt.Errorf("Delivery to local/link-local address %s is blocked (set %s=1 if delivery to a local service is required)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

// notifyTransport adds only a dial guard to the default Transport. Clone preserves
// connection pooling, HTTP/2, timeouts, proxy behavior, and other tuning.
var notifyTransport = func() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	clone := t.Clone()
	clone.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: blockInternalDial}).DialContext
	return clone
}()

// httpClient is shared by notification channel deliveries.
//
// Do not reuse server GlobalProxy: it carries target traffic through potentially
// unstable tunnels. Notifications must not depend on target-network reliability.
// IM delivery uses its own route with a 15-second timeout; slower peers are failing.
//
// Reject cross-host redirects. These channels use fixed endpoints and embed
// credentials in URLs (DingTalk access_token, WeCom key, Telegram bot token), so
// following a cross-host redirect could disclose them. Same-host redirects, such
// as adding a trailing slash, remain allowed.
var httpClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: notifyTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("Too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("Cross-host redirect rejected (%s -> %s)", via[0].URL.Host, req.URL.Host)
		}
		return nil
	},
}

// respBodyLimit bounds response reads. Faulty peers may return large bodies, but
// delivery history needs only an error code and a short explanation.
const respBodyLimit = 8 << 10

// doJSON sends a request and returns a size-limited response body.
//
// A nil payload sends an empty body, for GET or APIs that do not require one.
// Attach headers unchanged for custom Webhook headers.
//
// Classify network failures and 5xx/408/429 responses as retryable, and other 4xx
// responses as permanent. Retrying 403 merely repeats the same error in the logs.
func doJSON(ctx context.Context, method, url string, headers map[string]string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			// Serialization failures are local configuration/type bugs; retrying cannot help.
			return nil, Permanent(fmt.Errorf("Failed to construct request body: %w", err))
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// An invalid URL is usually a configuration error and is permanent.
		// Do not propagate err because url.Parse includes the full URL.
		return nil, Permanent(fmt.Errorf("Invalid request URL: %s", redactRequestTarget(url)))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Connection refusal, DNS failure, and timeouts are usually transient; retry with backoff.
		//
		// Redact error text before returning it. http.Client.Do returns *url.Error with
		// the full URL, including credentials such as DingTalk access_token, WeCom key,
		// Feishu hook id, or Telegram /bot<token>/. Without redaction, they leak through
		// four surfaces: stored notification_deliveries.last_error, delivery-history API
		// responses (bypassing configuration masking), server logs, and test-send API 502 bodies.
		return nil, fmt.Errorf("Request failed: %s", redactTransportError(err))
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if readErr != nil {
		return nil, fmt.Errorf("Failed to read response: %w", readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	// Retry 429 rate limits and 408 timeouts; other 4xx configuration/permission errors are permanent.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
		return nil, fmt.Errorf("Remote rate limit or timeout (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("Remote service error (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	return nil, Permanent(fmt.Errorf("Remote server rejected the request (HTTP %d): %s", resp.StatusCode, snippet(raw)))
}

// snippet collapses response text into a short single line for errors. Storing
// raw newlines and excessive whitespace in last_error would disrupt history layouts.
func snippet(raw []byte) string {
	return OneLine(string(raw), 200)
}

// redactRequestTarget reduces URLs to scheme://host/... for error messages.
//
// Use this single, deliberately coarse policy: keep only scheme and host. There
// is no general safe way to identify which URL segment contains credentials:
//
//	DingTalk: query /robot/send?access_token=xxx
//	WeCom: query /cgi-bin/webhook/send?key=xxx
//	Feishu: final path segment /open-apis/bot/v2/hook/<hook_id>
//	Telegram: middle path segment /bot<token>/sendMessage
//
// Keeping additional useful-looking parts would require channel-specific rules,
// and any omission leaks credentials. The host suffices for DNS, connection, and
// certificate troubleshooting; masked suffixes in configuration identify the bot.
// Return a fixed placeholder on parse failure; never echo the original input.
func redactRequestTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(unparseable URL)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// redactTransportError removes the URL and retains the underlying transport reason.
//
// *url.Error contains {Op, URL, Err}; Error() includes URL. Read Err directly
// instead of replacing strings afterward, which could miss encoded/escaped forms.
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
	// Other errors, such as redirect-policy failures, can also contain URLs; redact them too.
	return redactURLsInText(err.Error())
}

// redactURLsInText replaces http(s) URLs with redacted forms in unstructured errors,
// including redirect-policy or third-party errors. Recognize only http/https and
// split on whitespace and quotes, which cannot appear unescaped in URLs.
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
