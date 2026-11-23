package windows

import (
	"testing"
	"time"
)

func TestApprovalProgressWaitsForBrowserApproval(t *testing.T) {
	var progress approvalProgress

	action, seconds := progress.step(time.Now(), false, false)

	if action != approvalWaiting || seconds != 0 {
		t.Fatalf("action=%v seconds=%d", action, seconds)
	}
}

func TestApprovalProgressCountsDownWithoutRestartingOnRepeatedApproval(t *testing.T) {
	var progress approvalProgress
	now := time.Now()

	shown, initial := progress.step(now, true, false)
	running, remaining := progress.step(now.Add(time.Second), true, false)
	beforeDeadline, lastSecond := progress.step(now.Add(approvalStartDelay-time.Nanosecond), true, false)
	finished, zero := progress.step(now.Add(approvalStartDelay), true, false)

	if shown != approvalShowSuccess || initial != 10 || running != approvalCountdown || remaining != 9 {
		t.Fatalf("initial=%v,%d running=%v,%d", shown, initial, running, remaining)
	}
	if beforeDeadline != approvalCountdown || lastSecond != 1 || finished != approvalCloseStart || zero != 0 {
		t.Fatalf("before deadline=%v,%d at deadline=%v,%d", beforeDeadline, lastSecond, finished, zero)
	}
}

func TestApprovalProgressCancellationClosesWaitingPage(t *testing.T) {
	var progress approvalProgress

	action, _ := progress.step(time.Now(), false, true)

	if action != approvalCloseCancel {
		t.Fatalf("action=%v; want waiting-page cancellation", action)
	}
}

func TestApprovalProgressCancellationClosesSuccessPageWithoutWaitingForCountdown(t *testing.T) {
	var progress approvalProgress
	now := time.Now()

	progress.step(now, true, false)
	action, _ := progress.step(now.Add(time.Second), true, true)

	if action != approvalCloseStart {
		t.Fatalf("action=%v; want the success page's available close button", action)
	}
}
