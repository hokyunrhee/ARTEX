package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// HTTP error classification in doJSON.
//
// Adapters classify platform codes such as DingTalk errcode, Feishu code, and
// Telegram ok; doJSON separately classifies HTTP failures. Both layers matter:
// without this one, a gateway 503 could become permanent, while a 403 could waste
// three retry backoffs.

func replyServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDoJSONClassifiesHTTPStatus(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		permanent bool
	}{
		{"200 success is not an error", 200, false},
		{"429 rate limit is retryable", 429, false},
		{"408 request timeout is retryable", 408, false},
		{"500 server error is retryable", 500, false},
		{"502 gateway error is retryable", 502, false},
		{"503 service unavailable is retryable", 503, false},
		{"400 bad request is permanent", 400, true},
		{"401 authentication failure is permanent", 401, true},
		{"403 forbidden is permanent", 403, true},
		{"404 not found is permanent", 404, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := replyServer(t, tc.status, `{"detail":"upstream says no"}`)
			_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
			if tc.status < 300 {
				if err != nil {
					t.Fatalf("2xx must not return an error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Non-2xx must return an error")
			}
			if got := IsPermanent(err); got != tc.permanent {
				t.Fatalf("Incorrect permanent classification for HTTP %d: expected %v, got %v (%v)",
					tc.status, tc.permanent, got, err)
			}
			// Include the status code so operators can distinguish configuration errors from
			// remote outages. Assert the numeric code rather than Go StatusText; it remains
			// stable across message wording and language changes.
			if !strings.Contains(err.Error(), strconv.Itoa(tc.status)) {
				t.Errorf("Error must include HTTP status %d, got %v", tc.status, err)
			}
		})
	}
}

// TestDoJSONIncludesResponseSnippet verifies that errors include the remote
// explanation, so operators can understand why the request was rejected.
func TestDoJSONIncludesResponseSnippet(t *testing.T) {
	srv := replyServer(t, 400, `{"error":"invalid webhook token"}`)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("Expected an error")
	}
	if !strings.Contains(err.Error(), "invalid webhook token") {
		t.Errorf("Error must include the remote explanation, got %v", err)
	}
}

// TestDoJSONSnippetIsSingleLineAndBounded constrains text stored in last_error
// and shown in tables; multiline or oversized responses would disrupt layout and payloads.
func TestDoJSONSnippetIsSingleLineAndBounded(t *testing.T) {
	// Response with newlines, tabs, and 5000 characters.
	long := strings.Repeat("x", 5000)
	srv := replyServer(t, 500, "line1\nline2\r\n\tline3 "+long)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("Expected an error")
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\r\n\t") {
		t.Errorf("Error must be collapsed to one line, got %q", msg)
	}
	// The 200-character snippet plus fixed prefix must be far shorter than the original.
	if len(msg) > 400 {
		t.Errorf("Error is too long (%d bytes); snippet must truncate it: %q", len(msg), msg)
	}
}

// TestDoJSONRejectsOversizedResponse verifies bounded reads. Do not load an
// unexpectedly huge response into memory; each delivery also stores last_error.
func TestDoJSONRejectsOversizedResponse(t *testing.T) {
	huge := strings.Repeat("A", 1<<20) // 1 MiB
	srv := replyServer(t, 400, huge)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("Expected an error")
	}
	if len(err.Error()) > 400 {
		t.Errorf("Oversized responses must be read with a limit and truncated; error length %d", len(err.Error()))
	}
}
