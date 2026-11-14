package app

import (
	"context"
	"testing"
	"time"
)

func TestForwardSnapshotRunsWithAutomaticUploadsOff(t *testing.T) {
	application, client := fixtureApp(t)
	application.Config.Automatic = false
	state := fixtureCoordinator(t, application)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, fixtureFolderUpdate("accepted", "save"))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	request := nextRequest(t, ctx, client)
	request.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)

	if application.Config.Automatic || application.LastUpload.Fingerprint != "accepted" {
		t.Fatal("ready work did not finish independently of automatic polling")
	}
}

func TestForwardSnapshotDoesNotWaitForFutureEmission(t *testing.T) {
	application, client := fixtureApp(t)
	state := fixtureCoordinator(t, application)
	state.poller.requestEmission()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, fixtureFolderUpdate("existing", "old save"))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	request := nextRequest(t, ctx, client)

	if !state.poller.forceNextEmission || request.payload.Files["HERO_01_123.pld"].Text != "old save" {
		t.Fatal("existing delivery depended on a future emission")
	}
}

func TestUploadCommandOlderDeliveryDoesNotConsumeFreshRequest(t *testing.T) {
	application, client := fixtureApp(t)
	application.Config.Automatic = false
	state := fixtureCoordinator(t, application)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state.forwardSnapshot(ctx, fixtureFolderUpdate("old", "old save"))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	first := nextRequest(t, ctx, client)
	commandErr := state.handleCommand(ctx, Command{Kind: "upload"})
	first.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)
	beforeFresh := state.poller.forceNextEmission
	state.tick(ctx, time.Now().Add(6*time.Second))
	_ = nextCoordinatorEvent[uploadStarted](t, ctx, state)
	second := nextRequest(t, ctx, client)
	second.reply <- uploadReply{result: successfulResult()}
	_ = nextCoordinatorEvent[uploadSucceeded](t, ctx, state)

	if commandErr != nil {
		t.Fatal(commandErr)
	}
	if !beforeFresh || first.payload.Files["HERO_01_123.pld"].Text != "old save" {
		t.Fatal("old delivery consumed the fresh emission request")
	}
	if second.payload.Files["HERO_01_123.pld"].Text != "save" || application.LastUpload.Fingerprint != state.poller.lastEmittedFingerprint {
		t.Fatal("fresh snapshot did not finish")
	}
}
