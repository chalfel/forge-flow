package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Sync state
// ---------------------------------------------------------------------------

// SyncState tracks the current sync status and cached data.
type SyncState struct {
	mu            sync.RWMutex
	connected     bool
	lastSync      time.Time
	assignedTasks []CloudTask
	projects      []CloudProject
	knowledgeBase map[string]*KnowledgeBase // projectID -> KB
	listeners     []SyncListener
}

// SyncListener receives sync events.
type SyncListener interface {
	OnTaskAssigned(task CloudTask, spec CloudSpec)
	OnTaskUpdated(task CloudTask)
	OnSpecUpdated(spec CloudSpec)
	OnKnowledgeUpdated(projectID, section, content string)
	OnConnectionChange(connected bool)
}

// NewSyncState creates a new sync state manager.
func NewSyncState() *SyncState {
	return &SyncState{
		knowledgeBase: make(map[string]*KnowledgeBase),
	}
}

// AddListener registers a sync event listener.
func (s *SyncState) AddListener(l SyncListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, l)
}

// IsConnected returns true if we have an active cloud connection.
func (s *SyncState) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

// GetAssignedTasks returns cached assigned tasks.
func (s *SyncState) GetAssignedTasks() []CloudTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]CloudTask{}, s.assignedTasks...)
}

// GetKnowledge returns cached knowledge base for a project.
func (s *SyncState) GetKnowledge(projectID string) *KnowledgeBase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.knowledgeBase[projectID]
}

// ---------------------------------------------------------------------------
// Syncer orchestrates cloud synchronization
// ---------------------------------------------------------------------------

// Syncer manages bidirectional sync with Forge Cloud.
type Syncer struct {
	client    *Client
	state     *SyncState
	projectID string // current project context

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewSyncer creates a new syncer for a project.
func NewSyncer(client *Client, projectID string) *Syncer {
	return &Syncer{
		client:    client,
		state:     NewSyncState(),
		projectID: projectID,
	}
}

// State returns the sync state for inspection.
func (s *Syncer) State() *SyncState {
	return s.state
}

// Start begins the sync process.
func (s *Syncer) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Initial sync
	if err := s.initialSync(); err != nil {
		return fmt.Errorf("initial sync failed: %w", err)
	}

	// Register WebSocket handlers
	s.registerHandlers()

	// Connect WebSocket
	if err := s.client.Connect(s.ctx); err != nil {
		return fmt.Errorf("WebSocket connect failed: %w", err)
	}

	s.state.mu.Lock()
	s.state.connected = true
	s.state.mu.Unlock()
	s.notifyConnectionChange(true)

	// Start background goroutines
	s.wg.Add(1)
	go s.reconnectLoop()

	return nil
}

// Stop halts the sync process.
func (s *Syncer) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.client.Disconnect()
	s.wg.Wait()

	s.state.mu.Lock()
	s.state.connected = false
	s.state.mu.Unlock()
	s.notifyConnectionChange(false)
}

// initialSync fetches current state from the cloud.
func (s *Syncer) initialSync() error {
	// Fetch assigned tasks
	tasks, err := s.client.ListAssignedTasks()
	if err != nil {
		return fmt.Errorf("fetching assigned tasks: %w", err)
	}
	s.state.mu.Lock()
	s.state.assignedTasks = tasks
	s.state.lastSync = time.Now()
	s.state.mu.Unlock()

	// Fetch knowledge base if we have a project context
	if s.projectID != "" {
		kb, err := s.client.GetKnowledge(s.projectID)
		if err != nil {
			// Non-fatal, KB might not exist yet
		} else {
			s.state.mu.Lock()
			s.state.knowledgeBase[s.projectID] = kb
			s.state.mu.Unlock()
		}
	}

	return nil
}

// registerHandlers sets up WebSocket event handlers.
func (s *Syncer) registerHandlers() {
	s.client.OnEvent(MsgTypeTaskAssigned, s.handleTaskAssigned)
	s.client.OnEvent(MsgTypeTaskUpdated, s.handleTaskUpdated)
	s.client.OnEvent(MsgTypeSpecUpdated, s.handleSpecUpdated)
	s.client.OnEvent(MsgTypeKnowledgeUpdate, s.handleKnowledgeUpdate)
}

func (s *Syncer) handleTaskAssigned(msg WSMessage) {
	data, _ := json.Marshal(msg.Payload)
	var assignment TaskAssignment
	if err := json.Unmarshal(data, &assignment); err != nil {
		return
	}

	s.state.mu.Lock()
	s.state.assignedTasks = append(s.state.assignedTasks, assignment.Task)
	s.state.mu.Unlock()

	s.notifyTaskAssigned(assignment.Task, assignment.Spec)
}

func (s *Syncer) handleTaskUpdated(msg WSMessage) {
	data, _ := json.Marshal(msg.Payload)
	var task CloudTask
	if err := json.Unmarshal(data, &task); err != nil {
		return
	}

	s.state.mu.Lock()
	for i, t := range s.state.assignedTasks {
		if t.ID == task.ID {
			s.state.assignedTasks[i] = task
			break
		}
	}
	s.state.mu.Unlock()

	s.notifyTaskUpdated(task)
}

func (s *Syncer) handleSpecUpdated(msg WSMessage) {
	data, _ := json.Marshal(msg.Payload)
	var update SpecUpdate
	if err := json.Unmarshal(data, &update); err != nil {
		return
	}

	s.notifySpecUpdated(update.Spec)
}

func (s *Syncer) handleKnowledgeUpdate(msg WSMessage) {
	data, _ := json.Marshal(msg.Payload)
	var update KnowledgeUpdate
	if err := json.Unmarshal(data, &update); err != nil {
		return
	}

	// Update cache
	s.state.mu.Lock()
	kb := s.state.knowledgeBase[update.ProjectID]
	if kb == nil {
		kb = &KnowledgeBase{ProjectID: update.ProjectID}
		s.state.knowledgeBase[update.ProjectID] = kb
	}
	switch update.Section {
	case "constraints":
		kb.Constraints = update.Content
	case "memory":
		kb.Memory = update.Content
	case "architecture":
		kb.Architecture = update.Content
	case "business":
		kb.Business = update.Content
	case "conventions":
		kb.Conventions = update.Content
	default:
		if kb.Custom == nil {
			kb.Custom = make(map[string]string)
		}
		kb.Custom[update.Section] = update.Content
	}
	kb.UpdatedAt = update.Timestamp
	s.state.mu.Unlock()

	s.notifyKnowledgeUpdated(update.ProjectID, update.Section, update.Content)
}

// reconnectLoop handles reconnection with exponential backoff.
func (s *Syncer) reconnectLoop() {
	defer s.wg.Done()

	delay := ReconnectBaseDelay

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// Check if we're disconnected
		s.state.mu.RLock()
		connected := s.state.connected
		s.state.mu.RUnlock()

		if !connected {
			time.Sleep(delay)

			if err := s.client.Connect(s.ctx); err != nil {
				// Exponential backoff
				delay = delay * 2
				if delay > ReconnectMaxDelay {
					delay = ReconnectMaxDelay
				}
				continue
			}

			// Connected
			delay = ReconnectBaseDelay
			s.state.mu.Lock()
			s.state.connected = true
			s.state.mu.Unlock()
			s.notifyConnectionChange(true)

			// Re-sync after reconnect
			s.initialSync()
		}

		time.Sleep(5 * time.Second)
	}
}

// ---------------------------------------------------------------------------
// Outbound sync (CLI -> Cloud)
// ---------------------------------------------------------------------------

// SyncTaskStatus sends a status update to the cloud.
func (s *Syncer) SyncTaskStatus(taskID, status, branch, pr string) error {
	update := TaskStatusUpdate{
		TaskID:    taskID,
		Status:    status,
		Branch:    branch,
		PR:        pr,
		Timestamp: time.Now(),
	}

	// Update via REST
	if err := s.client.UpdateTaskStatus(taskID, update); err != nil {
		return err
	}

	// Also send via WebSocket for real-time update
	if s.state.IsConnected() {
		s.client.Send(WSMessage{
			Type:    MsgTypeStatusUpdate,
			Payload: update,
		})
	}

	return nil
}

// SyncTaskProgress sends a progress event to the cloud.
func (s *Syncer) SyncTaskProgress(taskID, msgType, message string, details any) error {
	event := TaskProgressEvent{
		TaskID:    taskID,
		Type:      msgType,
		Message:   message,
		Details:   details,
		Timestamp: time.Now(),
	}

	// Send via WebSocket if connected (real-time)
	if s.state.IsConnected() {
		return s.client.Send(WSMessage{
			Type:    MsgTypeProgress,
			Payload: event,
		})
	}

	// Fallback to REST
	return s.client.SendTaskProgress(event)
}

// ---------------------------------------------------------------------------
// Listener notifications
// ---------------------------------------------------------------------------

func (s *Syncer) notifyTaskAssigned(task CloudTask, spec CloudSpec) {
	s.state.mu.RLock()
	listeners := s.state.listeners
	s.state.mu.RUnlock()

	for _, l := range listeners {
		l.OnTaskAssigned(task, spec)
	}
}

func (s *Syncer) notifyTaskUpdated(task CloudTask) {
	s.state.mu.RLock()
	listeners := s.state.listeners
	s.state.mu.RUnlock()

	for _, l := range listeners {
		l.OnTaskUpdated(task)
	}
}

func (s *Syncer) notifySpecUpdated(spec CloudSpec) {
	s.state.mu.RLock()
	listeners := s.state.listeners
	s.state.mu.RUnlock()

	for _, l := range listeners {
		l.OnSpecUpdated(spec)
	}
}

func (s *Syncer) notifyKnowledgeUpdated(projectID, section, content string) {
	s.state.mu.RLock()
	listeners := s.state.listeners
	s.state.mu.RUnlock()

	for _, l := range listeners {
		l.OnKnowledgeUpdated(projectID, section, content)
	}
}

func (s *Syncer) notifyConnectionChange(connected bool) {
	s.state.mu.RLock()
	listeners := s.state.listeners
	s.state.mu.RUnlock()

	for _, l := range listeners {
		l.OnConnectionChange(connected)
	}
}
