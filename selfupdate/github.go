package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Repo is the release source. It is fixed rather than configurable: an editable update source would give anyone
// who can change configuration a remote code execution channel, which is unacceptable for a penetration-testing platform.
const Repo = "Autumn-27/artex"

// latestURL is GitHub's latest stable release endpoint; it automatically excludes prereleases and drafts.
const latestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// allowedHosts restricts update requests to these domains. Together with checkRedirect below,
// any redirect to an unlisted host fails immediately. This is the first barrier against DNS poisoning or
// a man-in-the-middle replacing the binary; SHA256SUMS comparison is the second.
var allowedHosts = map[string]bool{
	"api.github.com":                       true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true, // Object storage that serves release assets
	"release-assets.githubusercontent.com": true,
	"raw.githubusercontent.com":            true,
}

// Release contains the GitHub Release fields we need.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// Asset is a file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// NewClient constructs an HTTP client restricted to GitHub domains. An empty proxy uses a direct connection.
//
// Do not reuse the default Transport: updates must enforce TLS and certificate verification, unaffected by
// InsecureSkipVerify or similar settings configured elsewhere.
func NewClient(proxy string) *http.Client {
	tr := &http.Transport{
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if p := strings.TrimSpace(proxy); p != "" {
		if pu, err := url.Parse(p); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Minute, // Download the complete package; a short per-request timeout is unsuitable.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("Too many redirects")
			}
			return checkURL(req.URL)
		},
	}
}

// checkURL enforces HTTPS and the domain allowlist.
func checkURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("Non-HTTPS URL rejected: %s", u.Scheme+"://"+u.Host)
	}
	if !allowedHosts[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("Non-GitHub domain rejected: %s", u.Hostname())
	}
	return nil
}

// FetchLatest retrieves the latest stable release.
func FetchLatest(ctx context.Context, c *http.Client) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return nil, err
	}
	if err := checkURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "artex-selfupdate")

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Failed to reach GitHub (configure a global proxy in system settings if needed): %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests:
		// The unauthenticated GitHub API allows 60 requests per IP per hour, easily exhausted by shared outbound IPs.
		return nil, fmt.Errorf("GitHub API rate limit reached (60 requests per hour); try again later")
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("Repository %s has no stable releases yet", Repo)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub returned %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("Failed to parse release: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return nil, fmt.Errorf("Release is missing its tag")
	}
	return &rel, nil
}

// AssetName returns the current platform's release package name, matching build.sh's package_binary:
// artex-<version>-<os>-<arch>.zip (version without the v prefix).
func AssetName(tag, goos, goarch string) string {
	return fmt.Sprintf("artex-%s-%s-%s.zip", strings.TrimPrefix(tag, "v"), goos, goarch)
}

// FindAsset finds an asset by name in a release.
func (r *Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return Asset{}, false
}
