package app

import (
	"bytes"
	"context"
	"log"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"cwgrpguploader/internal/leaderboard"
)

func TestWorkLogContextPersistsAcrossRetriesAndChangesForNewEmissions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		var logs bytes.Buffer
		application.Logger = log.New(&logs, "", 0)
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		first := nextRequest(t, ctx, client)
		first.reply <- uploadReply{err: &leaderboard.UploadError{Message: "temporary failure", Retry: true, Body: "retry\nlater"}}
		retry := nextRequest(t, ctx, client)
		retry.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		err := sendCommand(t, ctx, application, Command{Kind: "upload"})
		second := nextRequest(t, ctx, client)
		second.reply <- uploadReply{result: successfulResult()}
		_ = awaitStatus(t, ctx, application, "successfully")
		cancel()
		<-done
		output := logs.String()
		accepted := regexp.MustCompile(`\[app\] work_id=(\S+) snapshot accepted: 2 eligible files`).FindAllStringSubmatch(output, -1)

		if err != nil {
			t.Fatal(err)
		}
		if len(accepted) != 2 || accepted[0][1] == accepted[1][1] {
			t.Fatalf("work IDs missing or reused: %s", output)
		}
		originalID, newID := accepted[0][1], accepted[1][1]
		for _, want := range []string{
			"[uploader] work_id=" + originalID + " upload failed: temporary failure",
			"[uploader] work_id=" + originalID + ` Upload error response body: "retry\nlater"`,
			"[uploader] work_id=" + originalID + " retry scheduled at ",
			"[uploader] work_id=" + originalID + " upload succeeded: 1 heroes",
			"[uploader] work_id=" + newID + " upload started: 2 eligible files",
			"[uploader] work_id=" + newID + " upload succeeded: 1 heroes",
		} {
			if !strings.Contains(output, want) {
				t.Errorf("missing %q in:\n%s", want, output)
			}
		}
		if strings.Count(output, "[uploader] work_id="+originalID+" upload started:") != 2 {
			t.Fatal("retry did not share the original correlation ID")
		}
	})
}

func TestWorkLogContextIncludesDryRunPreview(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, _ := fixtureApp(t)
		application.DryRun = true
		var logs bytes.Buffer
		application.Logger = log.New(&logs, "", 0)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = awaitStatus(t, ctx, application, "Dry run:")
		cancel()
		<-done

		if !regexp.MustCompile(`\[uploader\] work_id=\S+ dry-run snapshot prepared: 2 eligible files`).MatchString(logs.String()) {
			t.Fatalf("preview lost work context: %s", logs.String())
		}
	})
}

func TestWorkLogContextIncludesCancelledUpload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application, client := fixtureApp(t)
		var logs bytes.Buffer
		application.Logger = log.New(&logs, "", 0)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		done := startApp(ctx, application)
		_ = nextRequest(t, ctx, client)
		cancel()
		<-done

		if !regexp.MustCompile(`\[uploader\] work_id=\S+ upload stopped: context canceled`).MatchString(logs.String()) {
			t.Fatalf("cancellation lost work context: %s", logs.String())
		}
	})
}
