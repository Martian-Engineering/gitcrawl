package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openclaw/gitcrawl/internal/syncer"
)

func TestSyncProgressWriterPublishesPrivateSanitizedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	writer, err := newSyncProgressWriter(path, "openclaw/gitcrawl")
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	writer.now = func() time.Time {
		return time.Date(2026, 7, 31, 4, 0, 0, 0, time.UTC)
	}
	if err := writer.report(syncer.SyncProgress{
		Stage:                syncer.SyncProgressThreads,
		IssuesReceived:       56,
		PullRequestsReceived: 238,
		CommentsReceived:     350,
	}); err != nil {
		t.Fatalf("report progress: %v", err)
	}
	if err := writer.finish(syncProgressFailed); err != nil {
		t.Fatalf("finish progress: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("decode progress: %v", err)
	}
	if len(snapshot) != 8 ||
		snapshot["schema"] != syncProgressSchemaV1 ||
		snapshot["repository"] != "openclaw/gitcrawl" ||
		snapshot["state"] != string(syncProgressFailed) ||
		snapshot["stage"] != string(syncer.SyncProgressThreads) ||
		snapshot["issues_received"] != float64(56) ||
		snapshot["pull_requests_received"] != float64(238) ||
		snapshot["comments_received"] != float64(350) ||
		snapshot["observed_at"] != "2026-07-31T04:00:00Z" {
		t.Fatalf("progress snapshot = %#v", snapshot)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat progress: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("progress mode = %o", info.Mode().Perm())
	}
}

func TestSyncProgressWriterRequiresAbsolutePath(t *testing.T) {
	if _, err := newSyncProgressWriter(
		"progress.json",
		"openclaw/gitcrawl",
	); err == nil {
		t.Fatal("relative progress path unexpectedly accepted")
	}
}
