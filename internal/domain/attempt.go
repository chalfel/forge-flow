package domain

import "time"

type AttemptStatus string

const (
	StatusPreparingWorkspace     AttemptStatus = "preparing_workspace"
	StatusBuildingPrompt         AttemptStatus = "building_prompt"
	StatusLaunchingAgentProcess  AttemptStatus = "launching_agent_process"
	StatusInitializingSession    AttemptStatus = "initializing_session"
	StatusStreamingTurn          AttemptStatus = "streaming_turn"
	StatusFinishing              AttemptStatus = "finishing"
	StatusSucceeded              AttemptStatus = "succeeded"
	StatusFailed                 AttemptStatus = "failed"
	StatusTimedOut               AttemptStatus = "timed_out"
	StatusStalled                AttemptStatus = "stalled"
	StatusCanceledByReconcile    AttemptStatus = "canceled_by_reconciliation"
	// StatusDecomposed is returned by the captain agent when the issue was
	// successfully expanded into child tickets. The scheduler treats this
	// like Succeeded but additionally adds the parent to the skip set so it
	// is not re-dispatched while Symphony is running.
	StatusDecomposed AttemptStatus = "decomposed"
)

type RunAttempt struct {
	IssueID       string
	Attempt       *int
	WorkspacePath string
	StartedAt     time.Time
	Status        AttemptStatus
}

type LiveSession struct {
	SessionID    string
	ThreadID     string
	TurnID       string
	InputTokens  int
	OutputTokens int
	LastEventAt  time.Time
	StartedAt    time.Time
}

type RetryEntry struct {
	IssueID  string
	Attempt  int
	DueAtMs  int64
	Error    string
}
