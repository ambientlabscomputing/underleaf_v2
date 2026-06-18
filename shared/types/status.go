/*
How we use state and status:
	state: what is our current step in the process? (e.g. "provisioning", "running", "deleting")
	status: how far along are we in that step? ("in_progress", "succeeded", "failed")

	State is domain specific.
	Status is global: in_progress, succeeded, or failed.
*/

package types

// Status represents the progress of an operation or lifecycle step
// can only be "in_progress", "succeeded", or "failed".
type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
)
