package app

import (
	"errors"
	"testing"
	"time"

	"cwg2uploader/internal/saves"
	"cwg2uploader/internal/storage"
)

func TestRenderStatusWithHistory(t *testing.T) {
	history := storage.UploadState{Heroes: 1, UploadedAt: time.Date(2026, 1, 1, 12, 34, 56, 0, time.UTC)}
	cases := []struct {
		name string
		view statusView
		want string
	}{
		{"successful upload history", statusView{}, "Last upload: 1 heroes successfully uploaded at 2026-01-01 12:34:56"},
		{"success does not hide settling", statusView{folder: pollEvent{kind: pollSettling, err: saves.ErrSettling}}, "waiting for saves to settle; Last upload: 1 heroes successfully uploaded at 2026-01-01 12:34:56"},
		{"success does not hide pause", statusView{folder: pollEvent{kind: pollDisabled}}, "Automatic uploads off; Last upload: 1 heroes successfully uploaded at 2026-01-01 12:34:56"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message := renderStatus(tc.view, history)

			if message != tc.want {
				t.Fatalf("message = %q; want %q", message, tc.want)
			}
		})
	}
}

func TestRenderStatus(t *testing.T) {
	folderError := errors.New("cannot read save folder")
	rejected := errors.New("invalid save")
	persistenceError := errors.New("disk unavailable")
	deadline := time.Date(2026, 1, 1, 12, 34, 56, 0, time.UTC)
	cases := []struct {
		name string
		view statusView
		want string
	}{
		{"starting", statusView{}, "Starting; automatic uploads send saves to the public CWG2 leaderboard"},
		{"automatic off", statusView{folder: pollEvent{kind: pollDisabled}}, "Automatic uploads off"},
		{"settling", statusView{folder: pollEvent{kind: pollSettling, err: saves.ErrSettling}}, saves.ErrSettling.Error()},
		{"stable is detection only", statusView{folder: pollEvent{kind: pollStable}}, "No new changes"},
		{"pending", statusView{delivery: deliveryView{pending: &deliveryIdentity{}}}, "Pending upload"},
		{"active and scan error", statusView{folder: pollEvent{kind: pollFailed, err: folderError}, delivery: deliveryView{active: uploadStarted{}}}, "cannot read save folder; Uploading saves"},
		{"retry and automatic off", statusView{folder: pollEvent{kind: pollDisabled}, delivery: deliveryView{active: uploadRetryScheduled{retryAt: deadline}}}, "Automatic uploads off; Retry at 12:34:56"},
		{"pending behind old retry", statusView{saveFolder: "new", folder: pollEvent{kind: pollStable}, delivery: deliveryView{pending: &deliveryIdentity{saveFolder: "new"}, active: uploadRetryScheduled{saveFolder: "old", retryAt: deadline}}}, "No new changes; Pending upload; Retry at 12:34:56 (previous folder)"},
		{"rejection and automatic off", statusView{folder: pollEvent{kind: pollDisabled}, delivery: deliveryView{rejected: &uploadRejected{err: rejected}}}, "Automatic uploads off; invalid save; -save ingame or choose Upload now"},
		{"persistence warning during retry", statusView{delivery: deliveryView{active: uploadRetryScheduled{retryAt: deadline}}, persistenceError: persistenceError}, "Retry at 12:34:56; Could not persist successful state: disk unavailable"},
		{"dry run", statusView{delivery: deliveryView{preview: &uploadPreviewed{fileCount: 1}}}, "Dry run: 1 eligible files; no upload or successful state written"},
		{"command error and active", statusView{commandError: errors.New("cannot change folder"), delivery: deliveryView{active: uploadStarted{}}}, "cannot change folder; Uploading saves"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message := renderStatus(tc.view, storage.UploadState{})

			if message != tc.want {
				t.Fatalf("message = %q; want %q", message, tc.want)
			}
		})
	}
}
