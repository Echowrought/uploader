package app_test

import (
	"strings"
	"testing"
	"time"

	"cwgrpguploader/internal/app"
	"cwgrpguploader/internal/storage"
)

func TestMenuSeparatesDetectionDeliveryAndHistory(t *testing.T) {
	status := app.Status{
		Detection: "No new changes",
		Upload:    "Pending upload; Retry at 17:35:41 (previous folder)",
		Config:    storage.Config{SaveFolder: "selected saves"},
	}

	menu := status.Menu(time.Now())

	if menu.Detection.Title != "Detection: No new changes" ||
		menu.Upload.Title != "Upload: Pending upload; Retry at 17:35:41 (previous folder)" ||
		menu.History.Title != "Last success: Never" {
		t.Fatalf("independent statuses lost: %+v", menu)
	}
	if !strings.Contains(menu.Detection.Details, "selected saves") || !strings.Contains(menu.Upload.Details, "previous folder") {
		t.Fatalf("missing click details: %+v", menu)
	}
}

func TestMenuFormatsHistoryUsingLocalCalendarWithoutSeconds(t *testing.T) {
	utc := time.UTC
	local := time.FixedZone("viewer", 3*60*60)
	cases := []struct {
		name string
		now  time.Time
		at   time.Time
		want string
	}{
		{"today", time.Date(2026, 11, 16, 19, 0, 0, 0, utc), time.Date(2026, 11, 16, 17, 20, 49, 0, utc), "Last success: Today, 17:20"},
		{"previous calendar day", time.Date(2026, 11, 16, 0, 5, 0, 0, utc), time.Date(2026, 11, 15, 23, 50, 49, 0, utc), "Last success: 2026-11-15, 23:50"},
		{"previous year", time.Date(2026, 11, 16, 19, 0, 0, 0, utc), time.Date(2025, 11, 16, 17, 20, 49, 0, utc), "Last success: 2025-11-16, 17:20"},
		{"viewer timezone", time.Date(2026, 11, 16, 4, 0, 0, 0, local), time.Date(2026, 11, 15, 23, 20, 49, 0, utc), "Last success: Today, 02:20"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := app.Status{History: storage.UploadState{UploadedAt: tc.at, Player: "Player", Heroes: 2, SaveFolder: "delivered saves"}}

			menu := status.Menu(tc.now)

			if menu.History.Title != tc.want {
				t.Fatalf("title=%q want=%q", menu.History.Title, tc.want)
			}
			fullTime := tc.at.In(tc.now.Location()).Format("2006-01-02 15:04:05 -0700")
			if !strings.Contains(menu.History.Details, fullTime) || !strings.Contains(menu.History.Details, "Heroes: 2") || !strings.Contains(menu.History.Details, "delivered saves") {
				t.Fatalf("history detail incomplete: %s", menu.History.Details)
			}
		})
	}
}

func TestMenuKeepsConfirmedSuccessVisibleWhenHistoryCannotBeSaved(t *testing.T) {
	now := time.Date(2026, 11, 16, 17, 30, 0, 0, time.UTC)
	status := app.Status{
		Detection:    "Automatic uploads off",
		Upload:       "Uploading saves",
		History:      storage.UploadState{UploadedAt: now.Add(-10 * time.Minute)},
		HistoryError: "disk is full",
	}

	menu := status.Menu(now)

	if menu.History.Title != "Last success: Today, 17:20 (history not saved)" {
		t.Fatalf("confirmed delivery or persistence warning lost: %s", menu.History.Title)
	}
	if !strings.Contains(menu.History.Details, "disk is full") || !strings.Contains(menu.History.Details, "same saves again") {
		t.Fatalf("missing persistence detail: %s", menu.History.Details)
	}
	if menu.Detection.Title != "Detection: Automatic uploads off" || menu.Upload.Title != "Upload: Uploading saves" {
		t.Fatal("history failure obscured independent activity")
	}
}

func TestMenuShowsWhenCurrentSettledSavesWereFirstObserved(t *testing.T) {
	now := time.Date(2026, 11, 16, 18, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"today", time.Date(2026, 11, 16, 17, 20, 49, 0, time.UTC), "Detection: No changes since 17:20; Save warnings: 1 (see logs)"},
		{"older day", time.Date(2026, 11, 15, 17, 20, 49, 0, time.UTC), "Detection: No changes since 2026-11-15, 17:20; Save warnings: 1 (see logs)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := app.Status{Detection: "No new changes", DetectionSince: tc.at, DetectionWarnings: 1}

			menu := status.Menu(now)

			if menu.Detection.Title != tc.want || !strings.Contains(menu.Detection.Details, "17:20:49") || !strings.Contains(menu.Detection.Details, "monitoring session") {
				t.Fatalf("observation time or detail lost: %+v", menu.Detection)
			}
			if menu.History.Title != "Last success: Never" {
				t.Fatal("detection time was mistaken for delivery history")
			}
		})
	}
}

func TestMenuRetainsUntruncatedStatusDetails(t *testing.T) {
	detail := "read save folder: " + strings.Repeat("long folder name ", 20)
	status := app.Status{Detection: detail}

	menu := status.Menu(time.Now())

	if !strings.Contains(menu.Detection.Details, detail) {
		t.Fatal("full diagnostic detail was truncated before presentation")
	}
}
