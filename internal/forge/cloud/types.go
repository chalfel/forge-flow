// Package cloud provides the client for connecting forge-cli to Forge Cloud.
// It handles authentication, real-time sync of tasks/specs, and bidirectional
// status updates between the local CLI and the cloud orchestration layer.
package cloud

import "time"

// ---------------------------------------------------------------------------
// Core entities synced between cloud and CLI
// ---------------------------------------------------------------------------

// CloudTask represents a task as stored/managed in Forge Cloud.
// Maps to the local RunPlanTask but includes cloud-specific metadata.
type CloudTask struct {
	ID          string    `json:"id"`
	SpecID      string    `json:"specId"`
	Name        string    `json:"name"`
	Status      string    `json:"status"` // backlog, in_progress, in_review, done, blocked
	AssignedTo  string    `json:"assignedTo,omitempty"`
	Priority    int       `json:"priority,omitempty"`
	Deps        []string  `json:"deps,omitempty"`
	Repo        string    `json:"repo,omitempty"`
	Touches     []string  `json:"touches,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	PR          string    `json:"pr,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// CloudSpec represents a spec document in Forge Cloud.
type CloudSpec struct {
	ID          string      `json:"id"`
	ProjectID   string      `json:"projectId"`
	Title       string      `json:"title"`
	Branch      string      `json:"branch,omitempty"`
	Priority    string      `json:"priority,omitempty"`
	Status      string      `json:"status"` // draft, active, completed, archived
	Content     string      `json:"content,omitempty"` // full markdown content
	Tasks       []CloudTask `json:"tasks,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

// CloudProject represents a project in Forge Cloud.
type CloudProject struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Repos       []string `json:"repos,omitempty"`
	TeamMembers []string `json:"teamMembers,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ---------------------------------------------------------------------------
// Events (CLI -> Cloud)
// ---------------------------------------------------------------------------

// TaskStatusUpdate is sent when a task status changes locally.
type TaskStatusUpdate struct {
	TaskID    string    `json:"taskId"`
	Status    string    `json:"status"`
	Branch    string    `json:"branch,omitempty"`
	PR        string    `json:"pr,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TaskProgressEvent is sent for real-time progress updates.
type TaskProgressEvent struct {
	TaskID    string    `json:"taskId"`
	Message   string    `json:"message"`
	Type      string    `json:"type"` // progress, decision, blocked, need_info
	Details   any       `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// SessionHeartbeat keeps the cloud informed that a dev session is active.
type SessionHeartbeat struct {
	SessionID   string   `json:"sessionId"`
	ActiveTasks []string `json:"activeTasks"`
	Timestamp   time.Time `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Events (Cloud -> CLI)
// ---------------------------------------------------------------------------

// TaskAssignment is received when a task is assigned to this user.
type TaskAssignment struct {
	Task      CloudTask `json:"task"`
	Spec      CloudSpec `json:"spec"`
	Timestamp time.Time `json:"timestamp"`
}

// SpecUpdate is received when a spec's content or tasks change.
type SpecUpdate struct {
	Spec      CloudSpec `json:"spec"`
	ChangeType string   `json:"changeType"` // created, updated, deleted
	Timestamp time.Time `json:"timestamp"`
}

// KnowledgeUpdate is received when the project knowledge base changes.
type KnowledgeUpdate struct {
	ProjectID string    `json:"projectId"`
	Section   string    `json:"section"` // constraints, memory, architecture, etc.
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// WebSocket message envelope
// ---------------------------------------------------------------------------

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type      string `json:"type"`
	Payload   any    `json:"payload"`
	RequestID string `json:"requestId,omitempty"` // for request-response patterns
}

// Common message types
const (
	MsgTypeTaskAssigned    = "task.assigned"
	MsgTypeTaskUpdated     = "task.updated"
	MsgTypeSpecUpdated     = "spec.updated"
	MsgTypeKnowledgeUpdate = "knowledge.updated"
	MsgTypeHeartbeat       = "heartbeat"
	MsgTypeHeartbeatAck    = "heartbeat.ack"
	MsgTypeStatusUpdate    = "status.update"
	MsgTypeProgress        = "progress"
	MsgTypeError           = "error"
)
