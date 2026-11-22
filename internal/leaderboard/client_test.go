package leaderboard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/buildinfo"
)

func TestClientUploadsRequest(t *testing.T) {
	var captured UploadRequest
	var method, path, contentType, userAgent string
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, contentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		userAgent = r.UserAgent()
		decodeErr = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write([]byte(`{"player":"Player","heroes":1,"user_hash":"123","replaced":[],"kept_newer":[]}`))
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), URL: server.URL}
	request := UploadRequest{Files: map[string]UploadFile{"HERO_01_123.pld": {Text: "save", Mtime: 1700000000123}}}
	wantUserAgent := "CWGRPGUploader/" + buildinfo.Version

	result, err := uploadForTest(client, context.Background(), request)

	if err != nil || decodeErr != nil {
		t.Fatalf("errors: %v, %v", err, decodeErr)
	}
	if method != "POST" || path != "/api/uploader/upload" || contentType != "application/json" {
		t.Fatal("incorrect upload request")
	}
	if userAgent != wantUserAgent {
		t.Fatalf("User-Agent = %q; want %q", userAgent, wantUserAgent)
	}
	if captured.Files["HERO_01_123.pld"] != request.Files["HERO_01_123.pld"] {
		t.Fatal("payload changed")
	}
	if result.Player != "Player" || result.Heroes != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestClientClassifiesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), URL: server.URL}

	_, err := uploadForTest(client, context.Background(), UploadRequest{})

	var uploadError *UploadError
	if !errors.As(err, &uploadError) || !uploadError.Retry || uploadError.After != 120*time.Second {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientClassifiesIncompleteErrorResponses(t *testing.T) {
	cases := []struct {
		status  int
		retry   bool
		message string
	}{
		{http.StatusBadRequest, false, `upload rejected: HTTP 400: "partial"`},
		{http.StatusTooManyRequests, true, `upload rejected: HTTP 429: "partial"`},
		{http.StatusServiceUnavailable, true, `upload rejected: HTTP 503: "partial"`},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "120")
				w.Header().Set("Content-Length", "100")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("partial"))
			}))
			defer server.Close()
			client := Client{HTTP: server.Client(), URL: server.URL}

			_, err := uploadForTest(client, context.Background(), UploadRequest{})

			var uploadError *UploadError
			if !errors.As(err, &uploadError) {
				t.Fatalf("unexpected error: %v", err)
			}
			if uploadError.Retry != tc.retry || uploadError.After != 120*time.Second {
				t.Fatalf("lost HTTP error classification: %+v", uploadError)
			}
			if uploadError.Message != tc.message {
				t.Fatalf("message = %q; want %q", uploadError.Message, tc.message)
			}
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("lost response read error: %v", err)
			}
			if uploadError.Body != "" {
				t.Fatal("incomplete body retained as a full diagnostic body")
			}
		})
	}
}

func TestClientRejectsIncompleteSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"player":"Player","heroes":1,"user_hash":"123"}`))
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), URL: server.URL}

	_, err := uploadForTest(client, context.Background(), UploadRequest{})

	var uploadError *UploadError
	if !errors.As(err, &uploadError) || !uploadError.Retry {
		t.Fatalf("unexpected error: %v", err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("lost response read error: %v", err)
	}
	if uploadError.Message != "read upload response: unexpected EOF" {
		t.Fatalf("unexpected message: %q", uploadError.Message)
	}
}

func TestClientClassifiesOversizedResponses(t *testing.T) {
	cases := []struct {
		status int
		retry  bool
		after  time.Duration
		prefix string
	}{
		{http.StatusOK, false, 0, "server response exceeds safety limit:"},
		{http.StatusTooManyRequests, true, 120 * time.Second, "upload rejected: HTTP 429:"},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			body := strings.Repeat("x", responseSizeLimit+1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			client := Client{HTTP: server.Client(), URL: server.URL}

			_, err := uploadForTest(client, context.Background(), UploadRequest{})

			var uploadError *UploadError
			if !errors.As(err, &uploadError) {
				t.Fatalf("unexpected error: %v", err)
			}
			if uploadError.Retry != tc.retry || uploadError.After != tc.after {
				t.Fatalf("unexpected error classification: %+v", uploadError)
			}
			if !strings.HasPrefix(uploadError.Message, tc.prefix) {
				t.Fatalf("unexpected message: %q", uploadError.Message)
			}
			if uploadError.Body != "" {
				t.Fatal("oversized body retained for diagnostic logging")
			}
		})
	}
}

func TestClientRetainsSmallErrorBodyForLogging(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid save"}`))
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), URL: server.URL}

	_, err := uploadForTest(client, context.Background(), UploadRequest{})

	var uploadError *UploadError
	if !errors.As(err, &uploadError) || uploadError.Retry || uploadError.Message != `upload rejected: HTTP 400: "{\"error\":\"invalid save\"}"` || uploadError.Body != `{"error":"invalid save"}` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientErrorBodyLoggingSizeLimit(t *testing.T) {
	cases := []struct {
		name string
		size int
		want string
	}{
		{"below limit", 4095, strings.Repeat("x", 4095)},
		{"at limit", 4096, ""},
		{"above limit", 4097, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Repeat("x", tc.size)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusRequestTimeout)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			client := Client{HTTP: server.Client(), URL: server.URL}

			_, err := uploadForTest(client, context.Background(), UploadRequest{})

			var uploadError *UploadError
			if !errors.As(err, &uploadError) || !uploadError.Retry {
				t.Fatalf("unexpected error: %v", err)
			}
			if uploadError.Message != `upload rejected: HTTP 408: "`+strings.Repeat("x", 512)+`" … [truncated]` {
				t.Fatalf("missing bounded response excerpt: %s", uploadError.Message)
			}
			if uploadError.Body != tc.want {
				t.Fatalf("retained body length = %d; want %d", len(uploadError.Body), len(tc.want))
			}
		})
	}
}

func TestClientRejectsUnrecognizedSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) }))
	defer server.Close()
	client := Client{HTTP: server.Client(), URL: server.URL}

	_, err := uploadForTest(client, context.Background(), UploadRequest{})

	if err == nil {
		t.Fatal("unrecognized success was accepted")
	}
}

func TestClientReportsCancelledRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	client := Client{HTTP: server.Client(), URL: server.URL}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := uploadForTest(client, ctx, UploadRequest{})

	var uploadError *UploadError
	if !errors.As(err, &uploadError) || uploadError.Retry {
		t.Fatalf("unexpected cancellation error: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation cause: %v", err)
	}
}

func TestClientReportsExpiredCallerDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	client := Client{HTTP: server.Client(), URL: server.URL}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := uploadForTest(client, ctx, UploadRequest{})

	var uploadError *UploadError
	if !errors.As(err, &uploadError) || uploadError.Retry {
		t.Fatalf("unexpected deadline error: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost deadline cause: %v", err)
	}
}

func TestClientReportsCancellationWhileReadingResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := Client{
				URL: BaseURL,
				HTTP: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: status,
						Header:     http.Header{"Retry-After": {"120"}},
						Body: io.NopCloser(readFunc(func(_ []byte) (int, error) {
							cancel()
							return 0, io.ErrUnexpectedEOF
						})),
					}, nil
				})},
			}

			_, err := uploadForTest(client, ctx, UploadRequest{})

			var uploadError *UploadError
			if !errors.As(err, &uploadError) || uploadError.Retry || uploadError.After != 0 {
				t.Fatalf("unexpected cancellation error: %v", err)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation cause: %v", err)
			}
		})
	}
}

func TestClientRetriesHTTPTimeoutWithActiveCallerContext(t *testing.T) {
	ctx := context.Background()
	client := Client{
		URL: BaseURL,
		HTTP: &http.Client{
			Timeout: 20 * time.Millisecond,
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				defer request.Body.Close()
				<-request.Context().Done()
				return nil, request.Context().Err()
			}),
		},
	}

	_, err := uploadForTest(client, ctx, UploadRequest{})

	var uploadError *UploadError
	if !errors.As(err, &uploadError) || !uploadError.Retry {
		t.Fatalf("unexpected timeout error: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost timeout cause: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("caller context expired: %v", ctx.Err())
	}
}

func TestClientPreservesConnectionError(t *testing.T) {
	cause := errors.New("connection failed")
	client := Client{
		URL: BaseURL,
		HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			defer request.Body.Close()
			return nil, cause
		})},
	}

	_, err := uploadForTest(client, context.Background(), UploadRequest{})

	var uploadError *UploadError
	if !errors.As(err, &uploadError) || !uploadError.Retry {
		t.Fatalf("unexpected connection error: %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("lost connection cause: %v", err)
	}
}

func TestClientQuotesAndBoundsResponseExcerpts(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty", "", "upload rejected: HTTP 400"},
		{"controls", "timeout\nretry\x1b", `error: "timeout\nretry\x1b"`},
		{"exact limit", strings.Repeat("x", 512), `error: "` + strings.Repeat("x", 512) + `"`},
		{"over limit", strings.Repeat("x", 513), `error: "` + strings.Repeat("x", 512) + `" … [truncated]`},
		{"split rune", strings.Repeat("x", 511) + "€", `error: "` + strings.Repeat("x", 511) + `" … [truncated]`},
		{"complete rune", strings.Repeat("x", 509) + "€", `error: "` + strings.Repeat("x", 509) + `€"`},
		{"invalid UTF-8", "bad\xff", `error: "bad\xff"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := Client{URL: "https://fixture.invalid", HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				r.Body.Close()
				return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			want := strings.Replace(tc.want, "error:", "upload rejected: HTTP 400:", 1)

			_, err := uploadForTest(client, context.Background(), UploadRequest{})

			var uploadError *UploadError
			if !errors.As(err, &uploadError) || uploadError.Message != want {
				t.Fatalf("error = %v; want %q", err, want)
			}
		})
	}
}

func TestClientParsesRetryAfterHTTPDate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value func(time.Time) string
		want  time.Duration
	}{
		{"future", func(now time.Time) string { return now.Add(time.Minute).UTC().Format(http.TimeFormat) }, time.Minute},
		{"past", func(now time.Time) string { return now.Add(-time.Minute).UTC().Format(http.TimeFormat) }, 0},
		{"invalid", func(time.Time) string { return "not a date" }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				value := tc.value(time.Now())
				client := Client{URL: "https://fixture.invalid", HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					r.Body.Close()
					return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {value}}, Body: io.NopCloser(strings.NewReader(""))}, nil
				})}}

				_, err := uploadForTest(client, t.Context(), UploadRequest{})

				var uploadError *UploadError
				if !errors.As(err, &uploadError) || !uploadError.Retry || uploadError.After != tc.want {
					t.Fatalf("Retry-After classification: %v; want delay %s", err, tc.want)
				}
			})
		})
	}
}

func uploadForTest(c Client, ctx context.Context, request UploadRequest) (Result, error) {
	return c.upload(ctx, request, strings.Repeat("xY", 21)+"Z")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(data []byte) (int, error) { return f(data) }
