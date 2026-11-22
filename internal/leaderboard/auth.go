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
	ErrAuthenticationRequired = errors.New("authentication required; choose Authenticate or run --authenticate")
	ErrAuthenticationRejected = errors.New("authentication rejected; reconnect with Authenticate or --authenticate")
	ErrAuthCancelled          = errors.New("authentication cancelled")
	approvalQuery             = regexp.MustCompile(`^request=[A-Za-z0-9_-]{43}$`)
	pollCodePattern           = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

type Account struct {
	client     *Client
	dir        string
	mu         sync.Mutex
	credential storage.Credential
	rejected   bool
	pending    bool
}

func OpenAccount(httpClient *http.Client, dir string) (*Account, error) {
	return openAccount(&Client{HTTP: httpClient, URL: BaseURL}, dir)
}

func openAccount(client *Client, dir string) (*Account, error) {
	credential, err := storage.LoadCredential(dir)
	return &Account{client: client, dir: dir, credential: credential}, err
}

// Status never returns secrets and does not make network requests.
func (a *Account) Status() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.rejected:
		return "Reconnect required"
	case a.credential.Valid():
		return "Connected as " + a.credential.BattleTag
	default:
		return "Not connected"
	}
}

func (a *Account) Authenticate(ctx context.Context, present func(context.Context, string) error) error {
	a.mu.Lock()
	if a.pending {
		a.mu.Unlock()
		return errors.New("authentication already in progress")
	}
	a.pending = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.pending = false; a.mu.Unlock() }()
	credential, err := a.client.authenticate(ctx, present)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := storage.SaveCredential(a.dir, credential); err != nil {
		return fmt.Errorf("save uploader credential: %w", err)
	}
	a.credential, a.rejected = credential, false
	return nil
}

// Disconnect removes the local credential only; the server's unlink behavior is unchanged.
func (a *Account) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending {
		return errors.New("cancel authentication before disconnecting")
	}
	if err := storage.RemoveCredential(a.dir); err != nil {
		return fmt.Errorf("remove uploader credential: %w", err)
	}
	a.credential, a.rejected = storage.Credential{}, false
	return nil
}

func (a *Account) Upload(ctx context.Context, request UploadRequest) (Result, error) {
	a.mu.Lock()
	credential, rejected := a.credential, a.rejected
	a.mu.Unlock()
	if rejected {
		return Result{}, ErrAuthenticationRejected
	}
	if !credential.Valid() {
		return Result{}, ErrAuthenticationRequired
	}
	if !trustedURL(a.client.URL, "", false) {
		return Result{}, errors.New("invalid Leaderboard destination")
	}
	result, err := a.client.upload(ctx, request, credential.Token)
	if errors.Is(err, ErrAuthenticationRejected) {
		a.mu.Lock()
		if a.credential == credential {
			a.rejected = true
		}
		a.mu.Unlock()
	}
	return result, err
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

func (c *Client) authenticate(ctx context.Context, present func(context.Context, string) error) (storage.Credential, error) {
	var empty storage.Credential
	if !trustedURL(c.URL, "", false) {
		return empty, errors.New("invalid Leaderboard destination")
	}
	var attempt authAttempt
	began := time.Now()
	status, _, err := c.authPOST(ctx, c.URL+"/api/uploader/begin-auth", "", &attempt)
	if ctx.Err() != nil {
		return empty, ErrAuthCancelled
	}
	if err != nil {
		return empty, err
	}
	if status != http.StatusCreated {
		return empty, fmt.Errorf("could not start authentication (HTTP %d)", status)
	}
	if !trustedURL(attempt.RedirectLink, c.URL+"/api/uploader/approve", true) ||
		!trustedURL(attempt.PollLink, c.URL+"/api/uploader/poll-auth", false) || !pollCodePattern.MatchString(attempt.PollCode) ||
		strings.Contains(attempt.RedirectLink, attempt.PollCode) || attempt.ExpiresIn < 1 || attempt.ExpiresIn > 3600 || attempt.Interval < 1 || attempt.Interval >= attempt.ExpiresIn {
		return empty, errors.New("invalid Leaderboard authentication response")
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
		var response authResponse
		status, after, err := c.authPOST(ctx, attempt.PollLink, attempt.PollCode, &response)
		if err != nil {
			if !errors.Is(err, errAuthConnection) {
				return empty, err
			}
			delay = max(interval, min(delay*2, time.Minute))
			continue
		}
		delay = max(interval, after)
		switch {
		case status == http.StatusOK && response.Valid() && response.Token != attempt.PollCode:
			return response.Credential, nil
		case status == http.StatusBadRequest && response.Error == "authorization_pending", status == http.StatusTooManyRequests, status >= 500:
			continue
		case status == http.StatusBadRequest && response.Error == "access_denied":
			return empty, errors.New("authentication was denied in the browser")
		case status == http.StatusBadRequest && response.Error == "expired_token":
			return empty, errors.New("authentication expired or was already redeemed; start again")
		default:
			return empty, fmt.Errorf("authentication failed (HTTP %d)", status)
		}
	}
}

func authContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("authentication expired; start again")
	}
	return ErrAuthCancelled
}

var errAuthConnection = errors.New("authentication connection failed")

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
