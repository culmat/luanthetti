// Package contentdb is a polite client for the ContentDB REST API
// (https://content.luanti.org/help/api/): it identifies itself, never exceeds the configured
// requests per second, retries on throttling and server errors, and streams downloads.
package contentdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// DefaultURL is the public instance.
const DefaultURL = "https://content.luanti.org"

// Client talks to one ContentDB instance.
type Client struct {
	Base      string
	UserAgent string
	HTTP      *http.Client
	limiter   *rate.Limiter
	Retries   int           // attempts on 429/5xx, default 4
	Backoff   time.Duration // first retry delay, doubled each time; default 1s
	Requests  int           // requests made, for reporting
}

// New creates a client for base (DefaultURL when empty) limited to rps requests per second.
func New(base, userAgent string, rps float64) *Client {
	if base == "" {
		base = DefaultURL
	}
	if rps <= 0 || rps > 2 {
		rps = 2 // ContentDB etiquette
	}
	return &Client{
		Base:      strings.TrimRight(base, "/"),
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		limiter:   rate.NewLimiter(rate.Limit(rps), 1),
		Retries:   4,
		Backoff:   time.Second,
	}
}

// SetLimit changes the request rate without the 2 requests/s cap New applies. Only for
// servers you run yourself (tests, a local mirror); never for content.luanti.org.
func (c *Client) SetLimit(rps float64) { c.limiter = rate.NewLimiter(rate.Limit(rps), 1) }

// HTTPError is a non-2xx answer.
type HTTPError struct {
	URL    string
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("%s: HTTP %d", e.URL, e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// IsNotFound reports whether err is a 404.
func IsNotFound(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == http.StatusNotFound
}

// do performs a rate-limited GET with retries; the caller closes the body.
func (c *Client) do(ctx context.Context, rawURL string) (*http.Response, error) {
	retries := c.Retries
	if retries <= 0 {
		retries = 1
	}
	backoff := c.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.UserAgent)
		req.Header.Set("Accept", "application/json, */*")
		c.Requests++
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
		} else if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			lastErr = &HTTPError{URL: rawURL, Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil && secs > 0 && time.Duration(secs)*time.Second > backoff {
					backoff = time.Duration(secs) * time.Second
				}
			}
		} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return nil, &HTTPError{URL: rawURL, Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
		} else {
			return resp, nil
		}
		if attempt+1 < retries {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			backoff *= 2
		}
	}
	return nil, lastErr
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	u := c.Base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	resp, err := c.do(ctx, u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: %w", u, err)
	}
	return nil
}

// ListQuery filters /api/packages/.
type ListQuery struct {
	Q               string
	Types           []string // mod, game, txp
	Tags            []string
	Author          string
	Game            string // author/name of a game: packages that support it
	Sort            string // score, downloads, created_at, approved_at, name, title, last_release
	Order           string // asc | desc
	Limit           int
	Hide            []string // content flags to hide
	EngineVersion   string
	ProtocolVersion int
}

// ListPackages calls /api/packages/, the list the in-game client uses.
func (c *Client) ListPackages(ctx context.Context, q ListQuery) ([]ShortPackage, error) {
	v := url.Values{}
	if q.Q != "" {
		v.Set("q", q.Q)
	}
	for _, t := range q.Types {
		v.Add("type", t)
	}
	for _, t := range q.Tags {
		v.Add("tag", t)
	}
	for _, h := range q.Hide {
		v.Add("hide", h)
	}
	if q.Author != "" {
		v.Set("author", q.Author)
	}
	if q.Game != "" {
		v.Set("game", q.Game)
	}
	if q.Sort != "" {
		v.Set("sort", q.Sort)
	}
	if q.Order != "" {
		v.Set("order", q.Order)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.EngineVersion != "" {
		v.Set("engine_version", q.EngineVersion)
	}
	if q.ProtocolVersion > 0 {
		v.Set("protocol_version", strconv.Itoa(q.ProtocolVersion))
	}
	var out []ShortPackage
	if err := c.getJSON(ctx, "/api/packages/", v, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Package calls /api/packages/<author>/<name>/.
func (c *Client) Package(ctx context.Context, author, name string) (*Detail, error) {
	var d Detail
	if err := c.getJSON(ctx, "/api/packages/"+url.PathEscape(author)+"/"+url.PathEscape(name)+"/", nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Releases calls /api/packages/<author>/<name>/releases/ (newest first).
func (c *Client) Releases(ctx context.Context, author, name string) ([]Release, error) {
	var out []Release
	if err := c.getJSON(ctx, "/api/packages/"+url.PathEscape(author)+"/"+url.PathEscape(name)+"/releases/", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// LatestReleases calls /api/updates/: "author/name" (lower-case author, aliases included) to the
// latest release id compatible with the engine (all releases when engine is "").
func (c *Client) LatestReleases(ctx context.Context, engineVersion string, protocol int) (map[string]int64, error) {
	v := url.Values{}
	for _, t := range []string{"mod", "game", "txp"} {
		v.Add("type", t)
	}
	if protocol > 0 {
		v.Set("protocol_version", strconv.Itoa(protocol))
	}
	if engineVersion != "" {
		v.Set("engine_version", engineVersion)
	}
	var raw map[string]int64
	if err := c.getJSON(ctx, "/api/updates/", v, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(raw))
	for k, rel := range raw {
		out[strings.ToLower(k)] = rel
	}
	return out, nil
}

// Tags calls /api/tags/.
func (c *Client) Tags(ctx context.Context) ([]Tag, error) {
	var out []Tag
	if err := c.getJSON(ctx, "/api/tags/", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DownloadURL is the release zip, as the in-game client builds it.
func (c *Client) DownloadURL(author, name string, release int64) string {
	return fmt.Sprintf("%s/packages/%s/%s/releases/%d/download/", c.Base, url.PathEscape(author), url.PathEscape(name), release)
}

// Download streams a release zip into w and returns the byte count.
func (c *Client) Download(ctx context.Context, author, name string, release int64, w io.Writer) (int64, error) {
	resp, err := c.do(ctx, c.DownloadURL(author, name, release))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return io.Copy(w, resp.Body)
}

// Get fetches an arbitrary URL on the ContentDB host or its CDN (thumbnails, screenshots).
// The caller closes the body.
func (c *Client) Get(ctx context.Context, rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if !c.allowedHost(u.Host) {
		return nil, fmt.Errorf("refusing to fetch %s: not a ContentDB host", u.Host)
	}
	return c.do(ctx, rawURL)
}

func (c *Client) allowedHost(host string) bool {
	base, err := url.Parse(c.Base)
	if err != nil {
		return false
	}
	h := strings.ToLower(host)
	return h == strings.ToLower(base.Host) || h == "cdn."+strings.ToLower(base.Host) || h == "cdn.content.luanti.org"
}
