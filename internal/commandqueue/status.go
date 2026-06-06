package commandqueue

const (
	StatusQueued    = "queued"
	StatusLeased    = "leased"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusExpired   = "expired"
	StatusCancelled = "cancelled"
)
