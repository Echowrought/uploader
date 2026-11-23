package windows

import (
	"context"

	"cwgrpguploader/internal/leaderboard"
	"cwgrpguploader/internal/storage"
)

// Authenticate keeps approval polling and the native dialog alive together,
// publishing the credential only after the dialog confirms that uploads may start.
func Authenticate(ctx context.Context, account *leaderboard.Account, authenticator *leaderboard.Authenticator) error {
	credential, err := authenticateWithApproval(ctx, authenticator.Authenticate, presentApproval)
	if err != nil {
		return err
	}
	return account.SetCredential(credential)
}

func authenticateWithApproval(ctx context.Context, authenticate func(context.Context, func(context.Context, string) error) (storage.Credential, error), present func(context.Context, string, <-chan struct{}) error) (storage.Credential, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(chan string)
	type result struct {
		credential storage.Credential
		err        error
	}
	finished := make(chan result, 1)
	go func() {
		credential, err := authenticate(ctx, func(_ context.Context, link string) error {
			ready <- link
			return nil
		})
		finished <- result{credential, err}
	}()

	var link string
	select {
	case link = <-ready:
	case outcome := <-finished:
		return storage.Credential{}, outcome.err
	}
	approved := make(chan struct{})
	window := make(chan error, 1)
	go func() { window <- present(ctx, link, approved) }()
	select {
	case outcome := <-finished:
		if outcome.err != nil {
			cancel()
			<-window
			return storage.Credential{}, outcome.err
		}
		close(approved)
		if err := <-window; err != nil {
			return storage.Credential{}, err
		}
		if ctx.Err() != nil {
			return storage.Credential{}, leaderboard.ErrAuthCancelled
		}
		return outcome.credential, nil
	case err := <-window:
		cancel()
		<-finished
		if err == nil {
			err = leaderboard.ErrAuthCancelled
		}
		return storage.Credential{}, err
	}
}
