package leaderboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"cwgrpguploader/internal/buildinfo"
	"cwgrpguploader/internal/storage"
)

var (
	ErrAuthCancelled  = errors.New("authentication cancelled")
	errAuthConnection = errors.New("authentication connection failed")
	pollCodePattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// Authenticator runs one explicit authorization attempt at a time.
type Authenticator struct {
	client  *Client
	mu      sync.Mutex
	pending bool
}

// NewAuthenticator uses the client's HTTP transport and fixed destination.
func NewAuthenticator(client *Client) *Authenticator { return &Authenticator{client: client} }

// Authenticate returns an approved credential without saving or activating it.
// Callers decide when to publish it through Account.SetCredential.
func (a *Authenticator) Authenticate(ctx context.Context, present func(context.Context, string) error) (storage.Credential, error) {
	a.mu.Lock()
	if a.pending {
		a.mu.Unlock()
		return storage.Credential{}, errors.New("authentication already in progress")
	}
	a.pending = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.pending = false; a.mu.Unlock() }()

	var empty storage.Credential
	began := time.Now()
	attempt, err := a.client.beginAuth(ctx)
	if ctx.Err() != nil {
		return empty, authContextError(ctx)
	}
	if err != nil {
		return empty, err
	}
	ctx, cancel := context.WithDeadline(ctx, began.Add(time.Duration(attempt.ExpiresIn)*time.Second))
	defer cancel()
	if ctx.Err() != nil {
		return empty, authContextError(ctx)
	}
	if err := present(ctx, attempt.RedirectLink); err != nil {
		return empty, err
	}
	interval := time.Duration(attempt.Interval) * time.Second
	delay := interval
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return empty, authContextError(ctx)
		case <-timer.C:
		}
		response, status, after, err := a.client.pollAuth(ctx, attempt.PollCode)
		if ctx.Err() != nil {
			return empty, authContextError(ctx)
		}
		if err != nil {
			if !errors.Is(err, errAuthConnection) {
				return empty, err
			}
			delay = max(interval, min(delay*2, time.Minute))
			continue
		}
		delay = max(interval, after)
		switch {
		case status == http.StatusOK:
			if !response.Valid() || response.Token == attempt.PollCode {
				return empty, invalidAuthResponse("poll-auth", status, "invalid upload credential")
			}
			return response.Credential, nil
		case status == http.StatusBadRequest && response.Error == "authorization_pending", status == http.StatusTooManyRequests, status >= 500:
			continue
		case status == http.StatusBadRequest && response.Error == "access_denied":
			return empty, errors.New("authentication was denied in the browser")
		case status == http.StatusBadRequest && response.Error == "expired_token":
			return empty, errors.New("authentication expired or was already redeemed; start again")
		default:
			return empty, fmt.Errorf("Leaderboard poll-auth failed (HTTP %d)", status)
		}
	}
}

func authContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("authentication expired; start again")
	}
	return ErrAuthCancelled
}

type authAttempt struct {
	RedirectLink string `json:"redirectLink"`
	PollLink     string `json:"pollLink"`
	PollCode     string `json:"pollCode"`
	ExpiresIn    int    `json:"expiresIn"`
	Interval     int    `json:"interval"`
}

type authResponse struct {
	storage.Credential
	Error string `json:"error"`
}

func (c *Client) beginAuth(ctx context.Context) (authAttempt, error) {
	var attempt authAttempt
	if c.URL != BaseURL {
		return attempt, errors.New("invalid Leaderboard destination")
	}
	status, _, err := c.authPOST(ctx, "begin-auth", "", &attempt)
	if err != nil {
		return attempt, err
	}
	if status != http.StatusCreated {
		return attempt, fmt.Errorf("Leaderboard begin-auth failed (HTTP %d)", status)
	}
	var problem string
	switch {
	case attempt.PollLink != c.URL+"/api/uploader/poll-auth":
		problem = "unexpected polling endpoint"
	case !pollCodePattern.MatchString(attempt.PollCode):
		problem = "missing or invalid polling code"
	case attempt.ExpiresIn < 1 || attempt.ExpiresIn > 3600 || attempt.Interval < 1 || attempt.Interval >= attempt.ExpiresIn:
		problem = "invalid expiry or polling interval"
	default:
		problem = approvalLinkProblem(attempt.RedirectLink, c.URL, attempt.PollCode)
	}
	if problem != "" {
		return authAttempt{}, invalidAuthResponse("begin-auth", status, problem)
	}
	return attempt, nil
}

func (c *Client) pollAuth(ctx context.Context, pollCode string) (authResponse, int, time.Duration, error) {
	var response authResponse
	status, after, err := c.authPOST(ctx, "poll-auth", pollCode, &response)
	return response, status, after, err
}

func (c *Client) authPOST(ctx context.Context, stage, secret string, out any) (int, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/api/uploader/"+stage, nil)
	if err != nil {
		return 0, 0, errors.New("invalid authentication request")
	}
	req.Header.Set("User-Agent", "CWGRPGUploader/"+buildinfo.Version)
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	response, err := c.doPrivate(req)
	if err != nil {
		return 0, 0, fmt.Errorf("Leaderboard %s: %w", stage, errAuthConnection)
	}
	defer response.Body.Close()
	after := retryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated || response.StatusCode == http.StatusBadRequest {
		data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
		if err != nil {
			return 0, 0, fmt.Errorf("Leaderboard %s (HTTP %d): %w", stage, response.StatusCode, errAuthConnection)
		}
		if len(data) > 4096 {
			return 0, 0, invalidAuthResponse(stage, response.StatusCode, "response body exceeds size limit")
		}
		if json.Unmarshal(data, out) != nil {
			return 0, 0, invalidAuthResponse(stage, response.StatusCode, "expected a JSON response matching the authentication schema")
		}
	}
	return response.StatusCode, after, nil
}

func (c *Client) doPrivate(req *http.Request) (*http.Response, error) {
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(req)
}

func invalidAuthResponse(stage string, status int, reason string) error {
	// Never include server-supplied body, links, header values or tokens here.
	return fmt.Errorf("invalid Leaderboard authentication response (%s, HTTP %d): %s", stage, status, reason)
}

func approvalLinkProblem(raw, baseURL, pollCode string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || strings.Contains(raw, "#") {
		return "invalid browser approval link"
	}
	base, _ := url.Parse(baseURL) // The caller has already checked the configured URL against BaseURL.
	if u.Scheme != "https" || !(strings.EqualFold(u.Host, base.Host) || strings.EqualFold(u.Host, base.Host+":443")) {
		return "browser approval link outside the Leaderboard HTTPS origin"
	}
	mount := strings.TrimSuffix(base.Path, "/") + "/"
	// Reject normalization tricks (escaped slashes, dot segments and duplicate
	// separators) rather than trusting that the browser and server agree on them.
	if !strings.HasPrefix(u.Path, mount) || u.EscapedPath() != u.Path || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return "browser approval link outside the canonical Leaderboard path"
	}
	query, err := url.QueryUnescape(u.RawQuery)
	if err != nil {
		return "invalid browser approval query"
	}
	if strings.Contains(raw, pollCode) || strings.Contains(query, pollCode) {
		return "browser approval link contains the private polling code"
	}
	return ""
}
