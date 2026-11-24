package windows

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unsafe"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

func TestNativeApprovalDialogUsesPackedWindowsABI(t *testing.T) {
	const pointerSize = unsafe.Sizeof(uintptr(0))
	if got := unsafe.Sizeof(taskDialogConfig{}); got != 32+16*pointerSize {
		t.Fatalf("TASKDIALOGCONFIG size = %d", got)
	}
	if got := unsafe.Sizeof(taskDialogButton{}); got != 4+pointerSize {
		t.Fatalf("TASKDIALOG_BUTTON size = %d", got)
	}
	if runtime.GOARCH == "amd64" {
		// Loading the native entry point without showing a GUI also verifies
		// that the test executable received the Common Controls v6 manifest.
		if err := taskDialogIndirect.Find(); err != nil {
			t.Fatal(err)
		}
	}
	if err := approvalKernel32.NewProc("RtlMoveMemory").Find(); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalContentShowsTheFullLinkAsAClickableLabel(t *testing.T) {
	link := leaderboard.BaseURL + "/connect-uploader?request=" + strings.Repeat("A", 43)

	content := strings.ReplaceAll(approvalContent(link), "\r\n", "")

	if !strings.Contains(content, `<a href="approval">`+link+`</a>`) {
		t.Fatalf("full approval link was not displayed as a hyperlink: %s", content)
	}
}

func TestApprovalContentExplainsTheLeaderboardConsentStep(t *testing.T) {
	content := approvalContent(leaderboard.BaseURL + "/connect-uploader")

	for _, phrase := range []string{"sign in if prompted", "Check that the account shown is yours, then select Approve upload access", "Signing in alone does not grant access", "upload your CWG2 saves only"} {
		if !strings.Contains(content, phrase) {
			t.Errorf("missing instruction: %q", phrase)
		}
	}
}

func TestApprovalWaitsForTheWindowToConfirmStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shown, start := make(chan struct{}), make(chan struct{})
		expected := storage.Credential{Token: strings.Repeat("A", 43), BattleTag: "Tester#1234"}
		authenticate := func(ctx context.Context, present func(context.Context, string) error) (storage.Credential, error) {
			return expected, present(ctx, "approval link")
		}
		present := func(_ context.Context, _ string, approved <-chan struct{}) error {
			<-approved
			close(shown)
			<-start
			return nil
		}
		done := make(chan struct{})
		var credential storage.Credential
		var err error

		go func() {
			credential, err = authenticateWithApproval(t.Context(), authenticate, present)
			close(done)
		}()
		<-shown
		synctest.Wait()
		returnedBeforeStart := false
		select {
		case <-done:
			returnedBeforeStart = true
		default:
		}
		close(start)
		<-done

		if returnedBeforeStart || err != nil || credential != expected {
			t.Fatalf("returned before start=%v, error=%v, valid credential=%v", returnedBeforeStart, err, credential.Valid())
		}
	})
}

func TestApprovalConfirmationOutlivesDevicePollingDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		expected := storage.Credential{Token: strings.Repeat("A", 43), BattleTag: "Tester#1234"}
		authenticate := func(ctx context.Context, present func(context.Context, string) error) (storage.Credential, error) {
			pollCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			return expected, present(pollCtx, "approval link")
		}
		present := func(ctx context.Context, _ string, approved <-chan struct{}) error {
			<-approved
			time.Sleep(approvalStartDelay)
			return ctx.Err()
		}

		credential, err := authenticateWithApproval(t.Context(), authenticate, present)

		if err != nil || credential != expected {
			t.Fatalf("countdown lost approval after device polling ended: %v", err)
		}
	})
}

func TestApprovalProtocolFailureClosesWindow(t *testing.T) {
	cause := errors.New("authentication expired")
	closed := make(chan struct{})
	authenticate := func(ctx context.Context, present func(context.Context, string) error) (storage.Credential, error) {
		if err := present(ctx, "approval link"); err != nil {
			return storage.Credential{}, err
		}
		return storage.Credential{}, cause
	}
	present := func(ctx context.Context, _ string, _ <-chan struct{}) error {
		<-ctx.Done()
		close(closed)
		return ctx.Err()
	}

	credential, err := authenticateWithApproval(t.Context(), authenticate, present)
	<-closed

	if !errors.Is(err, cause) || credential.Valid() {
		t.Fatalf("credential valid=%v error=%v", credential.Valid(), err)
	}
}

func TestApprovalCancellationAfterBrowserApprovalDiscardsCredential(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	authenticate := func(ctx context.Context, present func(context.Context, string) error) (storage.Credential, error) {
		return storage.Credential{Token: strings.Repeat("A", 43), BattleTag: "Tester#1234"}, present(ctx, "approval link")
	}
	present := func(_ context.Context, _ string, approved <-chan struct{}) error {
		<-approved
		cancel()
		return nil
	}

	credential, err := authenticateWithApproval(ctx, authenticate, present)

	if !errors.Is(err, leaderboard.ErrAuthCancelled) || credential.Valid() {
		t.Fatalf("credential valid=%v error=%v", credential.Valid(), err)
	}
}

func TestApprovalWindowFailureStopsAuthentication(t *testing.T) {
	for _, cause := range []error{leaderboard.ErrAuthCancelled, errors.New("window crashed")} {
		t.Run(cause.Error(), func(t *testing.T) {
			stopped := make(chan struct{})
			authenticate := func(ctx context.Context, present func(context.Context, string) error) (storage.Credential, error) {
				if err := present(ctx, "approval link"); err != nil {
					return storage.Credential{}, err
				}
				<-ctx.Done()
				close(stopped)
				return storage.Credential{}, ctx.Err()
			}

			_, err := authenticateWithApproval(t.Context(), authenticate, func(context.Context, string, <-chan struct{}) error { return cause })

			if !errors.Is(err, cause) {
				t.Fatalf("window error lost: %v", err)
			}
			select {
			case <-stopped:
			default:
				t.Fatal("returned while authentication was still running")
			}
		})
	}
}
