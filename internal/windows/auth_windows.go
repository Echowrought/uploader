package windows

import (
	"context"

	"cwgrpguploader/internal/leaderboard"
)

// Keep the native dialog and polling alive together. The dialog confirms that
// uploads may resume only after browser approval and its local countdown.
func authenticateWithApproval(ctx context.Context, authenticate func(context.Context, func(context.Context, string) error, func() error) error, present func(context.Context, string, <-chan struct{}) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(chan string, 1)
	approved := make(chan struct{})
	confirmed := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- authenticate(ctx, func(_ context.Context, link string) error {
			ready <- link
			return nil
		}, func() error {
			close(approved)
			select {
			case <-confirmed:
				return nil
			case <-ctx.Done():
				return leaderboard.ErrAuthCancelled
			}
		})
	}()

	var link string
	select {
	case link = <-ready:
	case err := <-finished:
		return err
	}
	window := make(chan error, 1)
	go func() { window <- present(ctx, link, approved) }()
	select {
	case err := <-finished:
		cancel()
		<-window
		return err
	case err := <-window:
		if err != nil {
			cancel()
			<-finished
			return err
		}
		close(confirmed)
		return <-finished
	}
}
