package web

import (
	"strings"
	"testing"
	"time"

	"netis/internal/backup"
	"netis/internal/events"
	"netis/internal/store"
)

type fixedBackups backup.Status

func (f fixedBackups) Status() backup.Status { return backup.Status(f) }

func testServerBackups(t *testing.T, b BackupStatus) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewServer(st, events.NewBroker(), nil, nil, Options{Backups: b}), st
}

// With scheduled backups off, About says so and /metrics carries no backup
// series that an alert would read as "backups failing".
func TestBackupsOffInAboutAndMetrics(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	about := authedGet(t, srv, st, "/settings?tab=about").Body.String()
	if !strings.Contains(about, "Last backup") || !strings.Contains(about, "off (set NETIS_BACKUP_DIR)") {
		t.Errorf("About tab does not report backups as off:\n%s", about)
	}
	metrics := authedGet(t, srv, st, "/metrics").Body.String()
	if strings.Contains(metrics, "netis_last_backup") {
		t.Errorf("metrics report backups while they are off:\n%s", metrics)
	}
}

func TestBackupStatusInAboutAndMetrics(t *testing.T) {
	at := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)
	srv, st := testServerBackups(t, fixedBackups{
		LastAttempt: at, LastSuccess: at, OK: true, File: "netis-20260928-030000.db",
	})
	st.SetSetting(t.Context(), "onboarded", "1")
	about := authedGet(t, srv, st, "/settings?tab=about").Body.String()
	if !strings.Contains(about, `datetime="2026-09-28T03:00:00Z"`) || !strings.Contains(about, "(netis-20260928-030000.db)") {
		t.Errorf("About tab does not show the last backup:\n%s", about)
	}
	metrics := authedGet(t, srv, st, "/metrics").Body.String()
	for _, want := range []string{
		"# TYPE netis_last_backup_timestamp_seconds gauge",
		"netis_last_backup_timestamp_seconds 1790564400",
		"netis_last_backup_success 1",
	} {
		if !strings.Contains(metrics, want) {
			t.Errorf("metrics missing %q\n%s", want, metrics)
		}
	}
}

func TestBackupFailureInAboutAndMetrics(t *testing.T) {
	at := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)
	srv, st := testServerBackups(t, fixedBackups{LastAttempt: at, OK: false})
	st.SetSetting(t.Context(), "onboarded", "1")
	about := authedGet(t, srv, st, "/settings?tab=about").Body.String()
	if !strings.Contains(about, "see the server log: <time datetime=\"2026-09-28T03:00:00Z\"") {
		t.Errorf("About tab does not show the failure:\n%s", about)
	}
	metrics := authedGet(t, srv, st, "/metrics").Body.String()
	for _, want := range []string{"netis_last_backup_timestamp_seconds 0", "netis_last_backup_success 0"} {
		if !strings.Contains(metrics, want) {
			t.Errorf("metrics missing %q\n%s", want, metrics)
		}
	}
}
