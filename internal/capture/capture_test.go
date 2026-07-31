package capture

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/gitcrawl/internal/store"
)

func TestBuildExportsDeterministicCodeFreeThreads(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gitcrawl.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	repoID, err := st.UpsertRepository(ctx, store.Repository{
		Owner:        "openclaw",
		Name:         "gitcrawl",
		FullName:     "openclaw/gitcrawl",
		GitHubRepoID: "R_1",
		RawJSON:      `{"private":"repository-raw-marker"}`,
		UpdatedAt:    "2026-07-30T20:00:00Z",
	})
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	thread := store.Thread{
		RepoID:            repoID,
		GitHubID:          "PR_7",
		Number:            7,
		Kind:              "pull_request",
		State:             "open",
		Title:             "Add capture export",
		Body:              "The public pull request body.",
		AuthorLogin:       "alice",
		AuthorType:        "User",
		AuthorAssociation: "MEMBER",
		HTMLURL:           "https://github.com/openclaw/gitcrawl/pull/7",
		LabelsJSON:        `[{"name":"feature"},{"name":"api"}]`,
		AssigneesJSON:     `[{"login":"bob"}]`,
		RawJSON:           `{"patch":"thread-raw-patch-marker"}`,
		ContentHash:       "internal-thread-hash",
		IsDraft:           true,
		CreatedAtGitHub:   "2026-07-30T18:00:00Z",
		UpdatedAtGitHub:   "2026-07-30T19:00:00Z",
		UpdatedAt:         "2026-07-30T19:00:01Z",
	}
	thread.ID, err = st.UpsertThread(ctx, thread)
	if err != nil {
		t.Fatalf("upsert thread: %v", err)
	}
	comments := []store.Comment{
		{
			ThreadID:        thread.ID,
			GitHubID:        "RC_2",
			CommentType:     "pull_review_comment",
			AuthorLogin:     "carol",
			AuthorType:      "User",
			Body:            "Please explain the boundary.",
			RawJSON:         `{"diff_hunk":"review-diff-marker","path":"private.go"}`,
			CreatedAtGitHub: "2026-07-30T18:30:00Z",
			UpdatedAtGitHub: "2026-07-30T18:31:00Z",
		},
		{
			ThreadID:        thread.ID,
			GitHubID:        "RV_1",
			CommentType:     "pull_review",
			AuthorLogin:     "dana",
			AuthorType:      "User",
			Body:            "Approved after the explanation.",
			ReviewState:     "APPROVED",
			RawJSON:         `{"state":"APPROVED","commit_id":"review-commit-marker"}`,
			CreatedAtGitHub: "2026-07-30T18:40:00Z",
			UpdatedAtGitHub: "2026-07-30T18:40:00Z",
		},
	}
	for _, comment := range comments {
		if _, err := st.UpsertComment(ctx, comment); err != nil {
			t.Fatalf("upsert comment: %v", err)
		}
	}
	reserveComments(t, ctx, st, thread.ID, thread.UpdatedAtGitHub)
	if _, err := st.UpsertComment(ctx, store.Comment{
		ThreadID: thread.ID, GitHubID: "C_unseen", CommentType: "issue_comment",
		Body: "not-observed-comment-marker", RawJSON: "{}",
		CreatedAtGitHub: "2026-07-30T18:50:00Z",
		UpdatedAtGitHub: "2026-07-30T18:50:00Z",
	}); err != nil {
		t.Fatalf("upsert comment absent from observation: %v", err)
	}
	if _, err := st.DB().ExecContext(ctx, `
		insert into pull_request_files(
			thread_id, path, status, patch, raw_json, fetched_at
		) values (?, 'private.go', 'modified', 'file-patch-marker', '{}',
			'2026-07-30T19:00:00Z')
	`, thread.ID); err != nil {
		t.Fatalf("seed code-bearing row: %v", err)
	}
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
	if _, err := st.RecordRun(ctx, store.RunRecord{
		RepoID: repoID, Kind: "sync", Scope: "numbers:7", Status: "success",
		StartedAt: "2026-07-30T20:01:00Z", FinishedAt: "2026-07-30T20:02:00Z",
	}); err != nil {
		t.Fatalf("record newer targeted sync: %v", err)
	}
	if _, err := st.RecordRun(ctx, store.RunRecord{
		RepoID: repoID, Kind: "sync", Scope: "all", Status: "success",
		StartedAt: "2026-07-30T20:03:00Z", FinishedAt: "2026-07-30T20:04:00Z",
		StatsJSON: `{"limit":1}`,
	}); err != nil {
		t.Fatalf("record newer limited sync: %v", err)
	}

	first, err := Build(
		ctx, st, "openclaw/gitcrawl", "v0.8.8-test", testRateLimit(), "",
	)
	if err != nil {
		t.Fatalf("build capture: %v", err)
	}
	second, err := Build(
		ctx, st, "openclaw/gitcrawl", "v0.8.8-test", testRateLimit(), "",
	)
	if err != nil {
		t.Fatalf("rebuild capture: %v", err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first capture: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second capture: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("capture changed without source changes:\n%s\n%s", firstJSON, secondJSON)
	}
	if first.Schema != SchemaV1 || first.Repository.ID != "R_1" ||
		first.SyncedAt != "2026-07-30T20:00:00Z" ||
		first.RateLimit.Resource != "core" ||
		first.RateLimit.Remaining != 37 ||
		len(first.Threads) != 1 {
		t.Fatalf("capture metadata = %+v", first)
	}
	got := first.Threads[0]
	if got.CanonicalID != "PR_7" || got.ContentHash == "" ||
		len(got.Comments) != 2 || got.Labels[0] != "api" ||
		got.Assignees[0] != "bob" {
		t.Fatalf("thread capture = %+v", got)
	}
	for _, forbidden := range []string{
		"repository-raw-marker",
		"thread-raw-patch-marker",
		"review-diff-marker",
		"private.go",
		"review-commit-marker",
		"file-patch-marker",
		"not-observed-comment-marker",
	} {
		if strings.Contains(string(firstJSON), forbidden) {
			t.Fatalf("capture leaked forbidden value %q: %s", forbidden, firstJSON)
		}
	}
}

func testRateLimit() RateLimit {
	resetAt := "2026-07-30T22:00:00Z"
	return RateLimit{
		Resource:   "core",
		Limit:      5000,
		Remaining:  37,
		ResetAt:    &resetAt,
		ObservedAt: "2026-07-30T21:00:00Z",
	}
}

func TestBuildRequiresStableRepositoryIdentityAndSuccessfulSync(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gitcrawl.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := st.UpsertRepository(ctx, store.Repository{
		Owner:     "openclaw",
		Name:      "gitcrawl",
		FullName:  "openclaw/gitcrawl",
		UpdatedAt: "2026-07-30T20:00:00Z",
	}); err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	if _, err := Build(
		ctx, st, "openclaw/gitcrawl", "test", testRateLimit(), "",
	); err == nil ||
		!strings.Contains(err.Error(), "stable GitHub repository id") {
		t.Fatalf("missing repository identity error = %v", err)
	}
	if _, err := st.DB().ExecContext(ctx, `
		update repositories set github_repo_id = 'R_1'
		where full_name = 'openclaw/gitcrawl'
	`); err != nil {
		t.Fatalf("add repository identity: %v", err)
	}
	if _, err := Build(
		ctx, st, "openclaw/gitcrawl", "test", testRateLimit(), "",
	); err == nil ||
		!strings.Contains(err.Error(), "unbounded all-state list sync") {
		t.Fatalf("missing sync error = %v", err)
	}
	repository, err := st.RepositoryByFullName(ctx, "openclaw/gitcrawl")
	if err != nil {
		t.Fatalf("read repository: %v", err)
	}
	if _, err := st.RecordRun(ctx, store.RunRecord{
		RepoID: repository.ID, Kind: "sync", Scope: "numbers:1", Status: "success",
		StartedAt: "2026-07-30T19:59:00Z", FinishedAt: "2026-07-30T20:00:00Z",
		StatsJSON: `{"comments_included":false}`,
	}); err != nil {
		t.Fatalf("record metadata-only sync: %v", err)
	}
	if _, err := Build(
		ctx, st, "openclaw/gitcrawl", "test", testRateLimit(), "",
	); err == nil ||
		!strings.Contains(err.Error(), "unbounded all-state list sync") {
		t.Fatalf("targeted sync freshness error = %v", err)
	}
	if _, err := st.RecordRun(ctx, store.RunRecord{
		RepoID: repository.ID, Kind: "sync", Scope: "all", Status: "success",
		StartedAt: "2026-07-30T20:01:00Z", FinishedAt: "2026-07-30T20:02:00Z",
	}); err != nil {
		t.Fatalf("record all-state sync: %v", err)
	}
	result, err := Build(
		ctx, st, "openclaw/gitcrawl", "test", testRateLimit(), "",
	)
	if err != nil || len(result.Threads) != 0 ||
		result.SyncedAt != "2026-07-30T20:02:00Z" {
		t.Fatalf("empty repository capture = %+v, %v", result, err)
	}
}

func TestBuildRequiresCurrentCommentObservationForEveryThread(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gitcrawl.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	repoID, err := st.UpsertRepository(ctx, store.Repository{
		Owner: "openclaw", Name: "gitcrawl", FullName: "openclaw/gitcrawl",
		GitHubRepoID: "R_1", UpdatedAt: "2026-07-30T20:00:00Z",
	})
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	thread := store.Thread{
		RepoID: repoID, GitHubID: "I_1", Number: 1, Kind: "issue",
		State: "open", Title: "coverage", Body: "body",
		HTMLURL:    "https://github.com/openclaw/gitcrawl/issues/1",
		LabelsJSON: "[]", AssigneesJSON: "[]", RawJSON: "{}", ContentHash: "one",
		UpdatedAtGitHub: "2026-07-30T19:00:00Z",
		UpdatedAt:       "2026-07-30T19:00:00Z",
	}
	thread.ID, err = st.UpsertThread(ctx, thread)
	if err != nil {
		t.Fatalf("upsert thread: %v", err)
	}
	if _, err := st.RecordRun(ctx, store.RunRecord{
		RepoID: repoID, Kind: "sync", Scope: "all", Status: "success",
		StartedAt: "2026-07-30T19:59:00Z", FinishedAt: "2026-07-30T20:00:00Z",
	}); err != nil {
		t.Fatalf("record sync: %v", err)
	}
	if _, err := Build(
		ctx, st, "openclaw/gitcrawl", "test", testRateLimit(), "",
	); err == nil ||
		!strings.Contains(err.Error(), "no complete comment observation") {
		t.Fatalf("missing comment observation error = %v", err)
	}
	reserveComments(t, ctx, st, thread.ID, "2026-07-30T18:00:00Z")
	if _, err := Build(
		ctx, st, "openclaw/gitcrawl", "test", testRateLimit(), "",
	); err == nil ||
		!strings.Contains(err.Error(), "current source revision") {
		t.Fatalf("stale comment observation error = %v", err)
	}
	reserveComments(t, ctx, st, thread.ID, thread.UpdatedAtGitHub)
	if _, err := Build(
		ctx, st, "openclaw/gitcrawl", "test", testRateLimit(), "",
	); err != nil {
		t.Fatalf("capture current comment observation: %v", err)
	}
}

func TestCaptureSyncAtSelectsRunCoveringRequestedRange(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gitcrawl.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	repoID, err := st.UpsertRepository(ctx, store.Repository{
		Owner: "openclaw", Name: "gitcrawl", FullName: "openclaw/gitcrawl",
		RawJSON: "{}", UpdatedAt: "2026-07-30T20:00:00Z",
	})
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	for _, run := range []store.RunRecord{
		{
			RepoID: repoID, Kind: "sync", Scope: "all", Status: "success",
			StartedAt: "2026-07-30T20:00:00Z", FinishedAt: "2026-07-30T20:01:00Z",
		},
		{
			RepoID: repoID, Kind: "sync", Scope: "all", Status: "success",
			StartedAt: "2026-07-30T20:02:00Z", FinishedAt: "2026-07-30T20:03:00Z",
			StatsJSON: `{"requested_since":"2026-07-01T00:00:00Z"}`,
		},
		{
			RepoID: repoID, Kind: "sync", Scope: "all", Status: "success",
			StartedAt: "2026-07-30T20:04:00Z", FinishedAt: "2026-07-30T20:05:00Z",
			StatsJSON: `{"requested_since":"2026-07-20T00:00:00Z"}`,
		},
		{
			RepoID: repoID, Kind: "sync", Scope: "all", Status: "success",
			StartedAt: "2026-07-30T20:06:00Z", FinishedAt: "2026-07-30T20:07:00Z",
			StatsJSON: `{"limit":1}`,
		},
	} {
		if _, err := st.RecordRun(ctx, run); err != nil {
			t.Fatalf("record sync run: %v", err)
		}
	}
	tests := []struct {
		name  string
		since string
		want  string
	}{
		{name: "full", want: "2026-07-30T20:01:00Z"},
		{
			name: "mid range", since: "2026-07-15T00:00:00Z",
			want: "2026-07-30T20:03:00Z",
		},
		{
			name: "recent range", since: "2026-07-25T00:00:00Z",
			want: "2026-07-30T20:05:00Z",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			since, err := parseSince(test.since)
			if err != nil {
				t.Fatalf("parse since: %v", err)
			}
			got, err := captureSyncAt(ctx, st, repoID, since)
			if err != nil || got.UTC().Format("2006-01-02T15:04:05Z") != test.want {
				t.Fatalf("capture sync = %s, %v; want %s", got, err, test.want)
			}
		})
	}
}

func TestBuildFiltersSinceAndRejectsMalformedSourceData(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gitcrawl.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
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
	for _, thread := range []store.Thread{
		{
			RepoID: repoID, GitHubID: "I_1", Number: 1, Kind: "issue",
			State: "closed", Title: "old", Body: "old body",
			HTMLURL:    "https://github.com/openclaw/gitcrawl/issues/1",
			LabelsJSON: `["bug"]`, AssigneesJSON: `["alice"]`,
			RawJSON: "{}", ContentHash: "old",
			UpdatedAtGitHub: "2026-07-01T00:00:00Z",
			UpdatedAt:       "2026-07-01T00:00:00Z",
		},
		{
			RepoID: repoID, GitHubID: "I_2", Number: 2, Kind: "issue",
			State: "open", Title: "new", Body: "new body",
			HTMLURL:    "https://github.com/openclaw/gitcrawl/issues/2",
			LabelsJSON: `["feature"]`, AssigneesJSON: `["bob"]`,
			RawJSON: "{}", ContentHash: "new",
			CreatedAtGitHub: "2026-07-20T00:00:00Z",
			UpdatedAtGitHub: "2026-07-30T00:00:00Z",
			UpdatedAt:       "2026-07-30T00:00:00Z",
		},
	} {
		threadID, err := st.UpsertThread(ctx, thread)
		if err != nil {
			t.Fatalf("upsert thread: %v", err)
		}
		reserveComments(t, ctx, st, threadID, thread.UpdatedAtGitHub)
	}
	if _, err := st.RecordRun(ctx, store.RunRecord{
		RepoID: repoID, Kind: "sync", Scope: "all", Status: "success",
		StartedAt: "2026-07-30T19:59:00Z", FinishedAt: "2026-07-30T20:00:00Z",
		StatsJSON: `{"comments_included":true}`,
	}); err != nil {
		t.Fatalf("record sync: %v", err)
	}
	result, err := Build(
		ctx,
		st,
		"openclaw/gitcrawl",
		"test",
		testRateLimit(),
		"2026-07-15T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("build bounded capture: %v", err)
	}
	if len(result.Threads) != 1 || result.Threads[0].Number != 2 {
		t.Fatalf("bounded threads = %+v", result.Threads)
	}
	if _, err := Build(
		ctx,
		st,
		"openclaw/gitcrawl",
		"test",
		testRateLimit(),
		"not-a-time",
	); err == nil || !strings.Contains(err.Error(), "capture since") {
		t.Fatalf("invalid since error = %v", err)
	}
	if _, err := st.DB().ExecContext(ctx, `
		update threads set updated_at_gh = 'not-a-time' where number = 2
	`); err != nil {
		t.Fatalf("corrupt source timestamp: %v", err)
	}
	if _, err := Build(
		ctx, st, "openclaw/gitcrawl", "test", testRateLimit(), "",
	); err == nil ||
		!strings.Contains(err.Error(), "thread #2 updated_at") {
		t.Fatalf("invalid source timestamp error = %v", err)
	}
}

// reserveComments records that a thread's comments were fully observed at its
// current source revision.
func reserveComments(
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

func TestBuildCommentRejectsUnknownKindAndClearsDeletedBody(t *testing.T) {
	if _, err := buildComment(7, store.Comment{
		GitHubID: "X_1", CommentType: "diff_context",
	}); err == nil || !strings.Contains(err.Error(), "unsupported comment kind") {
		t.Fatalf("unknown comment error = %v", err)
	}
	if _, err := buildComment(7, store.Comment{
		CommentType: "issue_comment",
	}); err == nil || !strings.Contains(err.Error(), "without a stable GitHub id") {
		t.Fatalf("missing comment id error = %v", err)
	}
	comment, err := buildComment(7, store.Comment{
		GitHubID: "C_1", CommentType: "issue_comment",
		Body: "deleted private body", DeletedAt: "2026-07-30T20:00:00Z",
	})
	if err != nil {
		t.Fatalf("build deleted comment: %v", err)
	}
	if comment.Body != "" || comment.DeletedAt != "2026-07-30T20:00:00Z" {
		t.Fatalf("deleted comment = %+v", comment)
	}
}
