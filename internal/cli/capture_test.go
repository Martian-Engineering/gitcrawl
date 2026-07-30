package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/gitcrawl/internal/capture"
	"github.com/openclaw/gitcrawl/internal/config"
	"github.com/openclaw/gitcrawl/internal/store"
)

func TestCaptureCommandWritesV1Atomically(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	dbPath := filepath.Join(root, "gitcrawl.db")
	cfg := config.Default()
	cfg.DBPath = dbPath
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	repoID, err := st.UpsertRepository(ctx, store.Repository{
		Owner:        "openclaw",
		Name:         "gitcrawl",
		FullName:     "openclaw/gitcrawl",
		GitHubRepoID: "R_1",
		UpdatedAt:    "2026-07-30T20:00:00Z",
	})
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	threadID, err := st.UpsertThread(ctx, store.Thread{
		RepoID:          repoID,
		GitHubID:        "I_1",
		Number:          1,
		Kind:            "issue",
		State:           "open",
		Title:           "Capture this",
		Body:            "Body",
		HTMLURL:         "https://github.com/openclaw/gitcrawl/issues/1",
		LabelsJSON:      "[]",
		AssigneesJSON:   "[]",
		RawJSON:         "{}",
		ContentHash:     "source",
		UpdatedAtGitHub: "2026-07-30T19:00:00Z",
		UpdatedAt:       "2026-07-30T19:00:00Z",
	})
	if err != nil {
		t.Fatalf("upsert thread: %v", err)
	}
	reserveCaptureComments(t, ctx, st, threadID, "2026-07-30T19:00:00Z")
	if _, err := st.RecordRun(ctx, store.RunRecord{
		RepoID:     repoID,
		Kind:       "sync",
		Scope:      "all",
		Status:     "success",
		StartedAt:  "2026-07-30T19:59:00Z",
		FinishedAt: "2026-07-30T20:00:00Z",
		StatsJSON:  `{"comments_included":true}`,
	}); err != nil {
		t.Fatalf("record sync: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	outputPath := filepath.Join(root, "captures", "capture.json")
	if err := New().Run(ctx, []string{
		"--config", configPath,
		"capture", "openclaw/gitcrawl",
		"--schema", capture.SchemaV1,
		"--output", outputPath,
	}); err != nil {
		t.Fatalf("run capture: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	var got capture.Capture
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	if got.Schema != capture.SchemaV1 || got.Repository.ID != "R_1" ||
		len(got.Threads) != 1 {
		t.Fatalf("capture = %+v", got)
	}
	entries, err := os.ReadDir(filepath.Dir(outputPath))
	if err != nil {
		t.Fatalf("read output directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "capture.json" {
		t.Fatalf("temporary output remained: %+v", entries)
	}
}

func TestCaptureCommandRejectsUnknownSchema(t *testing.T) {
	err := New().Run(context.Background(), []string{
		"capture", "openclaw/gitcrawl", "--schema", "gitcrawl.capture.v2",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported capture schema") {
		t.Fatalf("schema error = %v", err)
	}
}

func TestCaptureCommandWritesJSONToStdout(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	dbPath := filepath.Join(root, "gitcrawl.db")
	cfg := config.Default()
	cfg.DBPath = dbPath
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	repoID, err := st.UpsertRepository(ctx, store.Repository{
		Owner: "openclaw", Name: "gitcrawl", FullName: "openclaw/gitcrawl",
		GitHubRepoID: "R_1", UpdatedAt: "2026-07-30T20:00:00Z",
	})
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	if _, err := st.RecordRun(ctx, store.RunRecord{
		RepoID: repoID, Kind: "sync", Scope: "all", Status: "success",
		StartedAt: "2026-07-30T19:59:00Z", FinishedAt: "2026-07-30T20:00:00Z",
		StatsJSON: `{"comments_included":true}`,
	}); err != nil {
		t.Fatalf("record sync: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	app := New()
	var stdout bytes.Buffer
	app.Stdout = &stdout
	if err := app.Run(ctx, []string{
		"--config", configPath, "capture", "openclaw/gitcrawl",
	}); err != nil {
		t.Fatalf("run capture: %v", err)
	}
	var got capture.Capture
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}
	if got.Schema != capture.SchemaV1 || got.Threads == nil {
		t.Fatalf("stdout capture = %+v", got)
	}
}

func TestWriteCaptureAtomicallyRejectsEmptyOutput(t *testing.T) {
	if err := writeCaptureAtomically("", []byte("{}")); err == nil ||
		!strings.Contains(err.Error(), "output path is empty") {
		t.Fatalf("empty output error = %v", err)
	}
}

// reserveCaptureComments records complete comment coverage for a CLI fixture.
func reserveCaptureComments(
	t *testing.T,
	ctx context.Context,
	st *store.Store,
	threadID int64,
	sourceUpdatedAt string,
) {
	t.Helper()
	sequence, err := st.NextThreadObservationSequence(ctx, sourceUpdatedAt)
	if err != nil {
		t.Fatalf("next observation sequence: %v", err)
	}
	applied, err := st.ReserveThreadChildObservation(
		ctx,
		threadID,
		store.ThreadChildComments,
		sourceUpdatedAt,
		sequence,
	)
	if err != nil || !applied {
		t.Fatalf("reserve comment observation = %t, %v", applied, err)
	}
	comments, err := st.ListComments(ctx, threadID)
	if err != nil {
		t.Fatalf("list observed comments: %v", err)
	}
	memberIDs := make([]int64, 0, len(comments))
	for _, comment := range comments {
		memberIDs = append(memberIDs, comment.ID)
	}
	if err := st.ReplaceThreadChildObservationMembers(
		ctx,
		threadID,
		store.ThreadChildComments,
		sequence,
		memberIDs,
	); err != nil {
		t.Fatalf("replace comment observation members: %v", err)
	}
}
