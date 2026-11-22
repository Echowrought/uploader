package leaderboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cwgrpguploader/internal/buildinfo"
)

const BaseURL = "https://cwg.echowrought.com/leaderboard"
const responseSizeLimit = (1 << 20)
const errorBodyLogLimit = 4 << 10
const errorBodyExcerptLimit = 512

type Client struct {
	HTTP *http.Client
	URL  string
}

type UploadFile struct {
	Text  string `json:"text"`
	Mtime int64  `json:"mtime"`
}

type UploadRequest struct {
	Files map[string]UploadFile `json:"files"`
}

type Result struct {
	Player    string            `json:"player"`
	Heroes    int               `json:"heroes"`
	UserHash  json.RawMessage   `json:"user_hash"`
	Replaced  []json.RawMessage `json:"replaced"`
	KeptNewer []json.RawMessage `json:"kept_newer"`
}

type UploadError struct {
	// Body is retained for diagnostic logging only when smaller than 4 KiB.
	Body    string
	Message string
	Cause   error
	Retry   bool
	After   time.Duration
}

func (e *UploadError) Error() string { return e.Message }

func (e *UploadError) Unwrap() error { return e.Cause }

func (c *Client) upload(ctx context.Context, request UploadRequest, token string) (Result, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/api/uploader/upload", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CWGRPGUploader/"+buildinfo.Version)
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := c.doPrivate(req)
	if err != nil {
		retry := true
		if ctxErr := ctx.Err(); ctxErr != nil {
			err, retry = ctxErr, false
		}
		return Result{}, &UploadError{Message: "upload connection failed: " + strings.ReplaceAll(err.Error(), token, "[redacted]"), Cause: err, Retry: retry}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return Result{}, ErrAuthenticationRejected
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, responseSizeLimit+1))
	// Do not expose echoed credentials in diagnostics, including at the truncation boundary.
	diagnostic := bytes.ReplaceAll(data, []byte(token), []byte("[redacted]"))
	if ctxErr := ctx.Err(); err != nil && ctxErr != nil {
		return Result{}, &UploadError{Message: "read upload response: " + ctxErr.Error(), Cause: ctxErr}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Status and retry headers take precedence over an incomplete error body.
		var errorBody string
		if err == nil && len(data) < errorBodyLogLimit {
			errorBody = string(diagnostic)
		}
		return Result{}, &UploadError{
			Body:    errorBody,
			Cause:   err,
			Message: withResponseExcerpt(fmt.Sprintf("upload rejected: HTTP %d", response.StatusCode), diagnostic),
			Retry:   response.StatusCode == 408 || response.StatusCode == 429 || response.StatusCode >= 500,
			After:   retryAfter(response.Header.Get("Retry-After"), time.Now()),
		}
	}
	if err != nil {
		return Result{}, &UploadError{Message: "read upload response: " + strings.ReplaceAll(err.Error(), token, "[redacted]"), Cause: err, Retry: true}
	}
	if len(data) > responseSizeLimit {
		return Result{}, &UploadError{Message: withResponseExcerpt("server response exceeds safety limit", diagnostic)}
	}
	var result Result
	if err := json.Unmarshal(diagnostic, &result); err != nil || result.Player == "" || result.Heroes < 0 || (result.Heroes == 0 && len(result.KeptNewer) == 0) || len(result.UserHash) == 0 || string(result.UserHash) == "null" {
		return Result{}, &UploadError{Message: withResponseExcerpt("unrecognized upload response; success not recorded", diagnostic), Cause: err}
	}
	return result, nil
}

func withResponseExcerpt(message string, data []byte) string {
	if len(data) == 0 {
		return message
	}
	end := 0
	for end < len(data) && end < errorBodyExcerptLimit {
		_, size := utf8.DecodeRune(data[end:])
		if end+size > errorBodyExcerptLimit {
			break
		}
		end += size
	}
	// Quoting escapes controls and invalid UTF-8 bytes without splitting valid runes.
	message += ": " + strconv.Quote(string(data[:end]))
	if end < len(data) {
		message += " … [truncated]"
	}
	return message
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}
