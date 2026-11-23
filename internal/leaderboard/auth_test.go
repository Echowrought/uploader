package leaderboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/storage"
)

const (
	approvalLink = BaseURL + "/connect-uploader?request=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	pollLink     = BaseURL + "/api/uploader/poll-auth"
	pollSecret   = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	issuedToken  = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
)

func TestAuthenticatePollsAtServerIntervalsWithoutActivatingCredential(t *testing.T) {
	dir := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var calls []observedRequest
		client := authClient(func(req *http.Request) *http.Response {
			calls = append(calls, observe(req, start))
			switch len(calls) {
			case 1:
				return authReply(201, beginResponse(approvalLink, pollLink))
			case 2:
				return authReply(400, `{"error":"authorization_pending"}`)
			case 3:
				res := authReply(429, `{"error":"slow_down"}`)
				res.Header.Set("Retry-After", "3")
				return res
			default:
				return authReply(200, `{"token":"`+issuedToken+`","battletag":"Tester#1234","user_hash":42}`)
			}
		})
		account, err := OpenAccount(dir)
		client.account = account
		authenticator := NewAuthenticator(client)
		if err != nil {
			t.Fatal(err)
		}
		var displayed string

		credential, err := authenticator.Authenticate(t.Context(), func(_ context.Context, link string) error { displayed = link; return nil })
		_, loadErr := storage.LoadCredential(dir)

		if err != nil || displayed != approvalLink || credential.Token != issuedToken || credential.BattleTag != "Tester#1234" {
			t.Fatalf("authenticate=%v, displayed=%q, credential valid=%t", err, displayed, credential.Valid())
		}
		if !errors.Is(loadErr, os.ErrNotExist) || !account.MissingCredential() {
			t.Fatal("authentication saved or activated the credential before publication")
		}
		want := []observedRequest{
			{url: BaseURL + "/api/uploader/begin-auth", method: "POST"},
			{url: pollLink, method: "POST", authorization: "Bearer " + pollSecret, after: time.Second},
			{url: pollLink, method: "POST", authorization: "Bearer " + pollSecret, after: 2 * time.Second},
			{url: pollLink, method: "POST", authorization: "Bearer " + pollSecret, after: 5 * time.Second},
		}
		if len(calls) != len(want) {
			t.Fatalf("got %d requests, want %d", len(calls), len(want))
		}
		for i := range want {
			if calls[i] != want[i] {
				t.Fatalf("request %d had wrong destination, authorization, method, or timing", i)
			}
		}
	})
}

func TestBeginAuthAllowsApprovalPageAndQueryChangesWithinLeaderboard(t *testing.T) {
	for _, link := range []string{
		BaseURL + "/api/uploader/approve?request=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		BaseURL + "/account/connection/confirm?attempt=opaque&language=en",
	} {
		client := authClient(func(*http.Request) *http.Response { return authReply(201, beginResponse(link, pollLink)) })

		attempt, err := client.beginAuth(t.Context())

		if err != nil || attempt.RedirectLink != link {
			t.Fatalf("approval page was not accepted: %v", err)
		}
	}
}

func TestAuthenticateRejectsUntrustedLinksBeforePresentingOrPolling(t *testing.T) {
	for _, tc := range []struct{ name, approval, poll, reason string }{
		{"foreign poll", approvalLink, "https://other.example/api/uploader/poll-auth", "unexpected polling endpoint"},
		{"foreign approval", strings.Replace(approvalLink, BaseURL, "https://other.example", 1), pollLink, "outside the Leaderboard HTTPS origin"},
		{"alternate https port", "https://cwg.echowrought.com:8443/leaderboard/connect-uploader", pollLink, "outside the Leaderboard HTTPS origin"},
		{"plain HTTP", strings.Replace(approvalLink, "https:", "http:", 1), pollLink, "outside the Leaderboard HTTPS origin"},
		{"adjacent mount", BaseURL + "-admin/approve", pollLink, "outside the canonical Leaderboard path"},
		{"outside mount", "https://cwg.echowrought.com/admin/approve", pollLink, "outside the canonical Leaderboard path"},
		{"dot segments", BaseURL + "/../admin/approve", pollLink, "outside the canonical Leaderboard path"},
		{"escaped dot segments", BaseURL + "/%2e%2e/admin/approve", pollLink, "outside the canonical Leaderboard path"},
		{"escaped slash", BaseURL + "/connect%2fuploader", pollLink, "outside the canonical Leaderboard path"},
		{"secret in approval", strings.Replace(approvalLink, strings.Repeat("A", 43), pollSecret, 1), pollLink, "contains the private polling code"},
		{"encoded secret in approval", BaseURL + "/connect-uploader?attempt=" + strings.Repeat("%42", 43), pollLink, "contains the private polling code"},
		{"query on poll", approvalLink, pollLink + "?other=1", "unexpected polling endpoint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			client := authClient(func(*http.Request) *http.Response {
				requests++
				return authReply(201, beginResponse(tc.approval, tc.poll))
			})
			client.account, _ = OpenAccount(t.TempDir())
			presented := false

			_, err := NewAuthenticator(client).Authenticate(t.Context(), func(context.Context, string) error { presented = true; return nil })

			if err == nil || !strings.Contains(err.Error(), "begin-auth, HTTP 201") || !strings.Contains(err.Error(), tc.reason) || presented || requests != 1 {
				t.Fatalf("missing diagnostic or unexpected traffic: error=%v, presented=%t, requests=%d", err, presented, requests)
			}
			if strings.Contains(err.Error(), pollSecret) || strings.Contains(err.Error(), tc.approval) || strings.Contains(err.Error(), tc.poll) {
				t.Fatal("authentication diagnostic exposed response data")
			}
		})
	}
}

func TestMalformedAuthenticationResponseReportsStageAndStatusWithoutBody(t *testing.T) {
	client := authClient(func(*http.Request) *http.Response { return authReply(201, `<html>`+pollSecret+`</html>`) })

	_, err := client.beginAuth(t.Context())

	if err == nil || !strings.Contains(err.Error(), "begin-auth, HTTP 201") || !strings.Contains(err.Error(), "expected a JSON response") || strings.Contains(err.Error(), pollSecret) {
		t.Fatal("expected a redacted begin-auth JSON diagnostic")
	}
}

func TestFailedReconnectPreservesCredentialAndDoesNotExposeServerBody(t *testing.T) {
	dir := t.TempDir()
	existing := saveTestCredential(t, dir)
	synctest.Test(t, func(t *testing.T) {
		client := authClient(func(req *http.Request) *http.Response {
			if req.URL.String() != pollLink {
				return authReply(201, beginResponse(approvalLink, pollLink))
			}
			return authReply(400, `{"error":"access_denied","detail":"`+pollSecret+` `+existing.Token+`"}`)
		})
		account, _ := OpenAccount(dir)
		client.account = account

		_, err := NewAuthenticator(client).Authenticate(t.Context(), func(context.Context, string) error { return nil })
		saved, loadErr := storage.LoadCredential(dir)

		if err == nil || err.Error() != "authentication was denied in the browser" || loadErr != nil || saved != existing || account.Status() != "Connected as Tester#1234" {
			t.Fatal("denial did not return a safe error and preserve the account")
		}
	})
}

func TestPollCancellationStopsWithoutSendingAnotherRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		requests := 0
		client := authClient(func(*http.Request) *http.Response {
			requests++
			return authReply(201, beginResponse(approvalLink, pollLink))
		})
		client.account, _ = OpenAccount(t.TempDir())
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		_, err := NewAuthenticator(client).Authenticate(ctx, func(context.Context, string) error { cancel(); return nil })

		if !errors.Is(err, ErrAuthCancelled) || requests != 1 {
			t.Fatalf("error=%v, requests=%d", err, requests)
		}
	})
}

func TestPendingAuthenticationExpiresWithoutReturningCredential(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := authClient(func(req *http.Request) *http.Response {
			if req.URL.String() == pollLink {
				return authReply(400, `{"error":"authorization_pending"}`)
			}
			return authReply(201, beginResponse(approvalLink, pollLink))
		})
		began := time.Now()

		credential, err := NewAuthenticator(client).Authenticate(t.Context(), func(context.Context, string) error { return nil })

		if err == nil || !strings.Contains(err.Error(), "authentication expired") || credential.Valid() {
			t.Fatalf("credential valid=%v error=%v", credential.Valid(), err)
		}
		if elapsed := time.Since(began); elapsed != 10*time.Second {
			t.Fatalf("expired after %v; want the device deadline", elapsed)
		}
	})
}

func TestAuthenticationConnectionFailureBacksOffBeforePollingAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		began := time.Now()
		var calls []time.Duration
		client := &Client{URL: BaseURL, HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls = append(calls, time.Since(began))
			switch len(calls) {
			case 1:
				return authReply(201, beginResponse(approvalLink, pollLink)), nil
			case 2:
				return nil, errors.New("connection reset")
			default:
				return authReply(200, `{"token":"`+issuedToken+`","battletag":"Tester#1234"}`), nil
			}
		})}}

		credential, err := NewAuthenticator(client).Authenticate(t.Context(), func(context.Context, string) error { return nil })

		if err != nil || credential.Token != issuedToken || !reflect.DeepEqual(calls, []time.Duration{0, time.Second, 3 * time.Second}) {
			t.Fatalf("error=%v polling times=%v", err, calls)
		}
	})
}

func TestOnlyOneAuthenticationAttemptCanRunAtATime(t *testing.T) {
	dir := t.TempDir()
	requests := 0
	client := authClient(func(*http.Request) *http.Response {
		requests++
		return authReply(201, beginResponse(approvalLink, pollLink))
	})
	client.account, _ = OpenAccount(dir)
	authenticator := NewAuthenticator(client)
	opened := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := authenticator.Authenticate(ctx, func(ctx context.Context, _ string) error {
			close(opened)
			<-ctx.Done()
			return ErrAuthCancelled
		})
		done <- err
	}()

	<-opened
	_, secondErr := authenticator.Authenticate(t.Context(), func(context.Context, string) error { return nil })
	cancel()
	firstErr := <-done

	if secondErr == nil || !errors.Is(firstErr, ErrAuthCancelled) || requests != 1 {
		t.Fatalf("first=%v, second=%v, requests=%d", firstErr, secondErr, requests)
	}
}

func TestUploadWithoutCredentialDoesNotSendRequest(t *testing.T) {
	calls := 0
	client := authClient(func(*http.Request) *http.Response {
		calls++
		return authReply(200, `{}`)
	})
	client.account, _ = OpenAccount(t.TempDir())

	_, err := client.Upload(t.Context(), UploadRequest{})

	if !errors.Is(err, ErrAuthenticationRequired) || calls != 0 {
		t.Fatalf("error=%v, HTTP requests=%d", err, calls)
	}
}

func TestUploadDoesNotFallBackWhenCredentialIsRejected(t *testing.T) {
	dir := t.TempDir()
	var calls []observedRequest
	client := authClient(func(req *http.Request) *http.Response {
		calls = append(calls, observe(req, time.Now()))
		return authReply(401, `{"error":"leave the Authorization header out"}`)
	})
	existing := saveTestCredential(t, dir)
	account, _ := OpenAccount(dir)
	client.account = account

	_, firstErr := client.Upload(t.Context(), UploadRequest{})
	_, secondErr := client.Upload(t.Context(), UploadRequest{})

	if !errors.Is(firstErr, ErrAuthenticationRejected) || !errors.Is(secondErr, ErrAuthenticationRejected) || account.Status() != "Reconnect required" {
		t.Fatalf("first=%v, second=%v, status=%q", firstErr, secondErr, account.Status())
	}
	if len(calls) != 1 || calls[0].url != BaseURL+"/api/uploader/upload" || calls[0].authorization != "Bearer "+existing.Token {
		t.Fatal("unexpected authenticated request or anonymous fallback")
	}
}

func TestUploadDoesNotFollowRedirectOrExposeEchoedCredential(t *testing.T) {
	dir := t.TempDir()
	existing := saveTestCredential(t, dir)
	requests := 0
	client := authClient(func(*http.Request) *http.Response {
		requests++
		res := authReply(307, "echo: "+existing.Token)
		res.Header.Set("Location", BaseURL+"/other")
		return res
	})
	client.account, _ = OpenAccount(dir)

	_, err := client.Upload(t.Context(), UploadRequest{})

	var uploadErr *UploadError
	if requests != 1 || !errors.As(err, &uploadErr) || strings.Contains(uploadErr.Error()+uploadErr.Body, existing.Token) {
		t.Fatal("followed a redirect or exposed the credential in upload diagnostics")
	}
}

func TestUploadAcceptsSuccessWhenServerKeepsAllNewerSaves(t *testing.T) {
	dir := t.TempDir()
	saveTestCredential(t, dir)
	client := authClient(func(*http.Request) *http.Response {
		return authReply(200, `{"player":"Tester#1234","user_hash":42,"heroes":0,"kept_newer":["hero"]}`)
	})
	client.account, _ = OpenAccount(dir)

	result, err := client.Upload(t.Context(), UploadRequest{})

	if err != nil || result.Heroes != 0 || len(result.KeptNewer) != 1 {
		t.Fatalf("result=%+v, error=%v", result, err)
	}
}

func TestUploadRejectionDoesNotInvalidateAReplacementCredential(t *testing.T) {
	dir := t.TempDir()
	previous := saveTestCredential(t, dir)
	account, err := OpenAccount(dir)
	if err != nil {
		t.Fatal(err)
	}
	replacement := storage.Credential{Token: issuedToken, BattleTag: "Replacement#1234"}
	var saveErr error
	var authorization []string
	client := authClient(func(req *http.Request) *http.Response {
		authorization = append(authorization, req.Header.Get("Authorization"))
		if len(authorization) == 1 {
			saveErr = account.SetCredential(replacement)
			return authReply(401, `{}`)
		}
		return authReply(200, `{"player":"Replacement#1234","user_hash":42,"heroes":1}`)
	})
	client.account = account

	_, rejected := client.Upload(t.Context(), UploadRequest{})
	_, accepted := client.Upload(t.Context(), UploadRequest{})

	if saveErr != nil || !errors.Is(rejected, ErrAuthenticationRejected) || accepted != nil || account.Status() != "Connected as Replacement#1234" {
		t.Fatalf("save=%v, rejected=%v, accepted=%v, status=%q", saveErr, rejected, accepted, account.Status())
	}
	if len(authorization) != 2 || authorization[0] != "Bearer "+previous.Token || authorization[1] != "Bearer "+replacement.Token {
		t.Fatal("uploads did not use the current credential")
	}
}

type observedRequest struct {
	url, method, authorization string
	after                      time.Duration
}

func observe(req *http.Request, start time.Time) observedRequest {
	return observedRequest{req.URL.String(), req.Method, req.Header.Get("Authorization"), time.Since(start)}
}

func authClient(handle func(*http.Request) *http.Response) *Client {
	return &Client{URL: BaseURL, HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Body != nil {
			req.Body.Close()
		}
		return handle(req), nil
	})}}
}

func authReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func beginResponse(approval, poll string) string {
	data, err := json.Marshal(authAttempt{RedirectLink: approval, PollLink: poll, PollCode: pollSecret, ExpiresIn: 10, Interval: 1})
	if err != nil {
		panic(fmt.Sprintf("marshal auth fixture: %v", err))
	}
	return string(data)
}

func saveTestCredential(t *testing.T, dir string) storage.Credential {
	t.Helper()
	credential := storage.Credential{BattleTag: "Tester#1234", Token: strings.Repeat("X", 43)}
	if err := storage.SaveCredential(dir, credential); err != nil {
		t.Fatal(err)
	}
	return credential
}
