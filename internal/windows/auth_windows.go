package windows

import "context"

// Poll while the approval window is open. A failed or cancelled window stops
// polling; a completed attempt closes the window even if the link was copied.
func authenticateWithApproval(ctx context.Context, authenticate func(context.Context, func(context.Context, string) error) error, present func(context.Context, string) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type approval struct {
		ctx  context.Context
		link string
	}
	ready := make(chan approval, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- authenticate(ctx, func(ctx context.Context, link string) error {
			ready <- approval{ctx, link}
			return nil
		})
	}()

	var request approval
	select {
	case request = <-ready:
	case err := <-finished:
		return err
	}
	window := make(chan error, 1)
	go func() { window <- present(request.ctx, request.link) }()
	select {
	case err := <-finished:
		cancel()
		<-window
		return err
	case err := <-window:
		// Opening the browser closes only the window. If the attempt ended
		// first, its context closes the window and its result takes priority.
		if err == nil || request.ctx.Err() != nil {
			return <-finished
		}
		cancel()
		<-finished
		return err
	}
}
