package syncer

// SyncProgressStage is one sanitized phase of repository synchronization.
type SyncProgressStage string

const (
	SyncProgressConnecting SyncProgressStage = "connecting"
	SyncProgressThreads    SyncProgressStage = "syncing_threads"
	SyncProgressFinalizing SyncProgressStage = "finalizing"
)

// SyncProgress reports content-free activity observed during one sync.
type SyncProgress struct {
	Stage                SyncProgressStage
	IssuesReceived       int
	PullRequestsReceived int
	CommentsReceived     int
}

// SyncProgressReporter accepts one sanitized repository activity snapshot.
type SyncProgressReporter func(SyncProgress) error
