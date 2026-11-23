package windows

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"cwgrpguploader/internal/leaderboard"
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
	link := leaderboard.BaseURL + "/api/uploader/approve?request=" + strings.Repeat("A", 43)

	content := strings.ReplaceAll(approvalContent(link), "\r\n", "")

	if !strings.Contains(content, `<a href="approval">`+link+`</a>`) {
		t.Fatalf("full approval link was not displayed as a hyperlink: %s", content)
	}
}

func TestApprovalWaitsForTheWindowToConfirmStart(t *testing.T) {
	shown := make(chan struct{})
	start := make(chan struct{})
	var activationBeforeStart bool
	authenticate := func(ctx context.Context, present func(context.Context, string) error, confirm func() error) error {
		if err := present(ctx, "approval link"); err != nil {
			return err
		}
		if err := confirm(); err != nil {
			return err
		}
		select {
		case <-start:
		default:
			activationBeforeStart = true
		}
		return nil
	}
	present := func(_ context.Context, _ string, approved <-chan struct{}) error {
		<-approved
		close(shown)
		<-start
		return nil
	}
	done := make(chan error, 1)

	go func() { done <- authenticateWithApproval(t.Context(), authenticate, present) }()
	<-shown
	close(start)
	err := <-done

	if err != nil || activationBeforeStart {
		t.Fatalf("error=%v, activated before confirmation=%v", err, activationBeforeStart)
	}
}

func TestApprovalWindowFailureStopsAuthentication(t *testing.T) {
	for _, cause := range []error{leaderboard.ErrAuthCancelled, errors.New("window crashed")} {
		t.Run(cause.Error(), func(t *testing.T) {
			stopped := make(chan struct{})
			authenticate := func(ctx context.Context, present func(context.Context, string) error, _ func() error) error {
				if err := present(ctx, "approval link"); err != nil {
					return err
				}
				<-ctx.Done()
				close(stopped)
				return ctx.Err()
			}

			err := authenticateWithApproval(t.Context(), authenticate, func(context.Context, string, <-chan struct{}) error { return cause })

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
