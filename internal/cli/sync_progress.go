package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/openclaw/gitcrawl/internal/syncer"
)

const syncProgressSchemaV1 = "gitcrawl.sync-progress.v1"

type syncProgressState string

const (
	syncProgressRunning   syncProgressState = "running"
	syncProgressSucceeded syncProgressState = "succeeded"
	syncProgressFailed    syncProgressState = "failed"
)

const (
	syncProgressConnecting = syncer.SyncProgressConnecting
	syncProgressThreads    = syncer.SyncProgressThreads
	syncProgressFinalizing = syncer.SyncProgressFinalizing
)

type syncProgressSnapshot struct {
	Schema               string                   `json:"schema"`
	Repository           string                   `json:"repository"`
	State                syncProgressState        `json:"state"`
	Stage                syncer.SyncProgressStage `json:"stage"`
	IssuesReceived       int                      `json:"issues_received"`
	PullRequestsReceived int                      `json:"pull_requests_received"`
	CommentsReceived     int                      `json:"comments_received"`
	ObservedAt           string                   `json:"observed_at"`
}

type syncProgressWriter struct {
	path       string
	repository string
	now        func() time.Time
	latest     syncer.SyncProgress
}

func newSyncProgressWriter(
	path string,
	repository string,
) (*syncProgressWriter, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("sync progress path must be absolute")
	}
	if strings.TrimSpace(repository) == "" {
		return nil, fmt.Errorf("sync progress repository is required")
	}
	return &syncProgressWriter{
		path:       path,
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (w *syncProgressWriter) report(progress syncer.SyncProgress) error {
	if w == nil {
		return nil
	}
	w.latest = progress
	return w.write(syncProgressRunning)
}

func (w *syncProgressWriter) finish(state syncProgressState) error {
	if w == nil {
		return nil
	}
	return w.write(state)
}

func (w *syncProgressWriter) write(state syncProgressState) error {
	snapshot := syncProgressSnapshot{
		Schema:               syncProgressSchemaV1,
		Repository:           w.repository,
		State:                state,
		Stage:                w.latest.Stage,
		IssuesReceived:       w.latest.IssuesReceived,
		PullRequestsReceived: w.latest.PullRequestsReceived,
		CommentsReceived:     w.latest.CommentsReceived,
		ObservedAt:           w.now().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode sync progress: %w", err)
	}
	data = append(data, '\n')
	if err := writeCaptureAtomically(w.path, data); err != nil {
		return fmt.Errorf("write sync progress: %w", err)
	}
	return nil
}
