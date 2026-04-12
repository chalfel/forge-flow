package cloud

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentialsExpiry(t *testing.T) {
	tests := []struct {
		name        string
		expiresAt   time.Time
		wantExpired bool
		wantRefresh bool
	}{
		{
			name:        "zero time - never expires",
			expiresAt:   time.Time{},
			wantExpired: false,
			wantRefresh: false,
		},
		{
			name:        "expired",
			expiresAt:   time.Now().Add(-1 * time.Hour),
			wantExpired: true,
			wantRefresh: true,
		},
		{
			name:        "expiring soon",
			expiresAt:   time.Now().Add(2 * time.Minute),
			wantExpired: false,
			wantRefresh: true,
		},
		{
			name:        "valid",
			expiresAt:   time.Now().Add(1 * time.Hour),
			wantExpired: false,
			wantRefresh: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds := Credentials{
				AccessToken: "test",
				ExpiresAt:   tt.expiresAt,
			}
			if got := creds.IsExpired(); got != tt.wantExpired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.wantExpired)
			}
			if got := creds.NeedsRefresh(); got != tt.wantRefresh {
				t.Errorf("NeedsRefresh() = %v, want %v", got, tt.wantRefresh)
			}
		})
	}
}

func TestSaveAndLoadCredentials(t *testing.T) {
	// Use a temp dir for credentials
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .forge dir
	os.MkdirAll(filepath.Join(tmpDir, ".forge"), 0o700)

	creds := Credentials{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		UserID:       "user-123",
		Email:        "test@example.com",
		TeamID:       "team-456",
		CloudURL:     "https://api.test.com",
	}

	if err := SaveCredentials(creds); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	loaded, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}

	if loaded.AccessToken != creds.AccessToken {
		t.Errorf("AccessToken = %v, want %v", loaded.AccessToken, creds.AccessToken)
	}
	if loaded.Email != creds.Email {
		t.Errorf("Email = %v, want %v", loaded.Email, creds.Email)
	}
	if loaded.UserID != creds.UserID {
		t.Errorf("UserID = %v, want %v", loaded.UserID, creds.UserID)
	}
}

func TestLoadCredentialsNotLoggedIn(t *testing.T) {
	// Use a temp dir with no credentials
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	_, err := LoadCredentials()
	if err == nil {
		t.Error("LoadCredentials() expected error when not logged in")
	}
}

func TestNewClient(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg)

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.baseURL != DefaultCloudURL {
		t.Errorf("baseURL = %v, want %v", client.baseURL, DefaultCloudURL)
	}
}

func TestSyncStateGetters(t *testing.T) {
	state := NewSyncState()

	// Initially empty
	if state.IsConnected() {
		t.Error("IsConnected() should be false initially")
	}
	if len(state.GetAssignedTasks()) != 0 {
		t.Error("GetAssignedTasks() should be empty initially")
	}
	if state.GetKnowledge("test") != nil {
		t.Error("GetKnowledge() should return nil initially")
	}
}

func TestCloudTaskStatus(t *testing.T) {
	task := CloudTask{
		ID:     "task-1",
		Name:   "Test task",
		Status: "in_progress",
	}

	if task.Status != "in_progress" {
		t.Errorf("Status = %v, want in_progress", task.Status)
	}
}

func TestWSMessageTypes(t *testing.T) {
	// Verify constants are defined
	types := []string{
		MsgTypeTaskAssigned,
		MsgTypeTaskUpdated,
		MsgTypeSpecUpdated,
		MsgTypeKnowledgeUpdate,
		MsgTypeHeartbeat,
		MsgTypeStatusUpdate,
		MsgTypeProgress,
		MsgTypeError,
	}

	for _, typ := range types {
		if typ == "" {
			t.Error("Message type constant is empty")
		}
	}
}
