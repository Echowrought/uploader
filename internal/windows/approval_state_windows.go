package windows

import "time"

const approvalStartDelay = 10 * time.Second

type approvalAction uint8

const (
	approvalWaiting approvalAction = iota
	approvalShowSuccess
	approvalCountdown
	approvalCloseCancel
	approvalCloseStart
)

// approvalProgress owns the dialog's phase and countdown, independent of Win32.
// The native dialog owns rendering and retries of failed window operations.
type approvalProgress struct {
	startAt time.Time
}

func (p *approvalProgress) step(now time.Time, approved, cancelled bool) (approvalAction, int) {
	if cancelled {
		if !p.startAt.IsZero() {
			return approvalCloseStart, 0
		}
		return approvalCloseCancel, 0
	}
	if !p.startAt.IsZero() {
		remaining := p.startAt.Sub(now)
		if remaining <= 0 {
			return approvalCloseStart, 0
		}
		return approvalCountdown, int((remaining + time.Second - 1) / time.Second)
	}
	if approved {
		p.startAt = now.Add(approvalStartDelay)
		return approvalShowSuccess, int(approvalStartDelay / time.Second)
	}
	return approvalWaiting, 0
}
