package leaderboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	approvalQuery     = regexp.MustCompile(`^request=[A-Za-z0-9_-]{43}$`)
	pollCodePattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// Authenticator runs one explicit authorization attempt at a time.
type Authenticator struct {
	client  *Client
	mu      sync.Mutex
	pending bool
}

// NewAuthenticator shares the account already attached to client.
func NewAuthenticator(client *Client) *Authenticator { return &Authenticator{client: client} }

func (a *Authenticator) Authenticate(ctx context.Context, present func(context.Context, string) error) error {
	a.mu.Lock()
	if a.pending {
		a.mu.Unlock()
		return errors.New("authentication already in progress")
	}
	a.pending = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.pending = false; a.mu.Unlock() }()

	began := time.Now()
	attempt, err := a.client.beginAuth(ctx)
	if ctx.Err() != nil {
		return authContextError(ctx)
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(ctx, began.Add(time.Duration(attempt.ExpiresIn)*time.Second))
	defer cancel()
	if ctx.Err() != nil {
		return authContextError(ctx)
	}
	if err := present(ctx, attempt.RedirectLink); err != nil {
		return err
	}
	interval := time.Duration(attempt.Interval) * time.Second
	delay := interval
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return authContextError(ctx)
		case <-timer.C:
		}
		response, status, after, err := a.client.pollAuth(ctx, attempt)
		if ctx.Err() != nil {
			return authContextError(ctx)
		}
		if err != nil {
			if !errors.Is(err, errAuthConnection) {
				return err
			}
			delay = max(interval, min(delay*2, time.Minute))
			continue
		}
		delay = max(interval, after)
		switch {
		case status == http.StatusOK && response.Valid() && response.Token != attempt.PollCode:
			return a.client.account.SetCredential(response.Credential)
		case status == http.StatusBadRequest && response.Error == "authorization_pending", status == http.StatusTooManyRequests, status >= 500:
			continue
		case status == http.StatusBadRequest && response.Error == "access_denied":
			return errors.New("authentication was denied in the browser")
		case status == http.StatusBadRequest && response.Error == "expired_token":
			return errors.New("authentication expired or was already redeemed; start again")
		default:
			return fmt.Errorf("authentication failed (HTTP %d)", status)
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
	if !trustedURL(c.URL, BaseURL, false) {
		return attempt, errors.New("invalid Leaderboard destination")
	}
	status, _, err := c.authPOST(ctx, c.URL+"/api/uploader/begin-auth", "", &attempt)
	if err != nil {
		return attempt, err
	}
	if status != http.StatusCreated {
		return attempt, fmt.Errorf("could not start authentication (HTTP %d)", status)
	}
	if !trustedURL(attempt.RedirectLink, c.URL+"/api/uploader/approve", true) ||
		!trustedURL(attempt.PollLink, c.URL+"/api/uploader/poll-auth", false) || !pollCodePattern.MatchString(attempt.PollCode) ||
		strings.Contains(attempt.RedirectLink, attempt.PollCode) || attempt.ExpiresIn < 1 || attempt.ExpiresIn > 3600 || attempt.Interval < 1 || attempt.Interval >= attempt.ExpiresIn {
		return authAttempt{}, errors.New("invalid Leaderboard authentication response")
	}
	return attempt, nil
}

func (c *Client) pollAuth(ctx context.Context, attempt authAttempt) (authResponse, int, time.Duration, error) {
	var response authResponse
	status, after, err := c.authPOST(ctx, attempt.PollLink, attempt.PollCode, &response)
	return response, status, after, err
}

func (c *Client) authPOST(ctx context.Context, target, secret string, out any) (int, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if err != nil {
		return 0, 0, errors.New("invalid authentication request")
	}
	req.Header.Set("User-Agent", "CWGRPGUploader/"+buildinfo.Version)
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	response, err := c.doPrivate(req)
	if err != nil {
		return 0, 0, errAuthConnection
	}
	defer response.Body.Close()
	after := retryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated || response.StatusCode == http.StatusBadRequest {
		data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
		if err != nil {
			return 0, 0, errAuthConnection
		}
		if len(data) > 4096 || json.Unmarshal(data, out) != nil {
			return 0, 0, errors.New("invalid Leaderboard authentication response")
		}
	}
	return response.StatusCode, after, nil
}

func (c *Client) doPrivate(req *http.Request) (*http.Response, error) {
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(req)
}

func trustedURL(raw, expected string, approval bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") || u.Opaque != "" || u.ForceQuery {
		return false
	}
	if expected != "" && strings.SplitN(raw, "?", 2)[0] != expected {
		return false
	}
	if approval {
		return approvalQuery.MatchString(u.RawQuery)
	}
	return u.RawQuery == ""
}
