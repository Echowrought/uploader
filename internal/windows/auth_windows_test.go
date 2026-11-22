package windows

import (
	"context"
	"errors"
	"testing"

	"cwgrpguploader/internal/leaderboard"
)

func TestApprovalWindowClosesWhenAuthenticationFinishes(t *testing.T) {
	shown := make(chan struct{})
	closed := make(chan struct{})
	authenticate := func(ctx context.Context, present func(context.Context, string) error) error {
		if err := present(ctx, "approval link"); err != nil {
			return err
		}
		<-shown
		return nil
	}
	present := func(ctx context.Context, _ string) error {
		close(shown)
		<-ctx.Done()
		close(closed)
		return leaderboard.ErrAuthCancelled
	}

	err := authenticateWithApproval(t.Context(), authenticate, present)

	if err != nil {
		t.Fatalf("successful authentication was treated as a cancellation: %v", err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("returned before the approval window closed")
	}
}

func TestApprovalWindowFailureStopsAuthentication(t *testing.T) {
	for _, cause := range []error{leaderboard.ErrAuthCancelled, errors.New("window crashed")} {
		t.Run(cause.Error(), func(t *testing.T) {
			stopped := make(chan struct{})
			authenticate := func(ctx context.Context, present func(context.Context, string) error) error {
				if err := present(ctx, "approval link"); err != nil {
					return err
				}
				<-ctx.Done()
				close(stopped)
				return ctx.Err()
			}

			err := authenticateWithApproval(t.Context(), authenticate, func(context.Context, string) error { return cause })

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
