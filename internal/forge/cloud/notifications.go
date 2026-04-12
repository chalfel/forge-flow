package cloud

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ---------------------------------------------------------------------------
// Desktop notifications
// ---------------------------------------------------------------------------

// NotificationLevel indicates the urgency of a notification.
type NotificationLevel string

const (
	NotifyInfo     NotificationLevel = "info"
	NotifySuccess  NotificationLevel = "success"
	NotifyWarning  NotificationLevel = "warning"
	NotifyError    NotificationLevel = "error"
)

// Notification represents a desktop notification.
type Notification struct {
	Title   string
	Message string
	Level   NotificationLevel
	Sound   bool
}

// Notify sends a desktop notification using the system's native notification system.
func Notify(n Notification) error {
	switch runtime.GOOS {
	case "darwin":
		return notifyMac(n)
	case "linux":
		return notifyLinux(n)
	default:
		// Fallback: just print to stderr
		fmt.Fprintf(os.Stderr, "[%s] %s: %s\n", n.Level, n.Title, n.Message)
		return nil
	}
}

// notifyMac sends a notification on macOS using osascript.
func notifyMac(n Notification) error {
	script := fmt.Sprintf(`display notification %q with title %q`, n.Message, n.Title)

	if n.Sound {
		script += ` sound name "default"`
	}

	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// notifyLinux sends a notification on Linux using notify-send.
func notifyLinux(n Notification) error {
	// Check if notify-send is available
	if _, err := exec.LookPath("notify-send"); err != nil {
		// Fallback to terminal bell + print
		fmt.Fprintf(os.Stderr, "\a[%s] %s: %s\n", n.Level, n.Title, n.Message)
		return nil
	}

	args := []string{n.Title, n.Message}

	// Set urgency based on level
	switch n.Level {
	case NotifyError:
		args = append(args, "-u", "critical")
	case NotifyWarning:
		args = append(args, "-u", "normal")
	default:
		args = append(args, "-u", "low")
	}

	// Set icon based on level
	icon := "dialog-information"
	switch n.Level {
	case NotifySuccess:
		icon = "dialog-positive" // or emblem-ok
	case NotifyWarning:
		icon = "dialog-warning"
	case NotifyError:
		icon = "dialog-error"
	}
	args = append(args, "-i", icon)

	// Set app name
	args = append(args, "-a", "Forge")

	cmd := exec.Command("notify-send", args...)
	return cmd.Run()
}

// ---------------------------------------------------------------------------
// Convenience functions for common notifications
// ---------------------------------------------------------------------------

// NotifyTaskAssigned sends a notification when a new task is assigned.
func NotifyTaskAssigned(taskName, specTitle string) error {
	return Notify(Notification{
		Title:   "New Task Assigned",
		Message: fmt.Sprintf("%s\n%s", taskName, specTitle),
		Level:   NotifyInfo,
		Sound:   true,
	})
}

// NotifyTaskCompleted sends a notification when a task completes.
func NotifyTaskCompleted(taskName string, success bool) error {
	level := NotifySuccess
	title := "Task Completed"
	if !success {
		level = NotifyError
		title = "Task Failed"
	}
	return Notify(Notification{
		Title:   title,
		Message: taskName,
		Level:   level,
		Sound:   true,
	})
}

// NotifyReviewRequired sends a notification when review feedback is received.
func NotifyReviewRequired(taskName string, issueCount int) error {
	return Notify(Notification{
		Title:   "Review Feedback",
		Message: fmt.Sprintf("%s: %d issues to address", taskName, issueCount),
		Level:   NotifyWarning,
		Sound:   true,
	})
}

// NotifyConnectionLost sends a notification when cloud connection is lost.
func NotifyConnectionLost() error {
	return Notify(Notification{
		Title:   "Forge Cloud",
		Message: "Connection lost. Reconnecting...",
		Level:   NotifyWarning,
		Sound:   false,
	})
}

// NotifyConnectionRestored sends a notification when cloud connection is restored.
func NotifyConnectionRestored() error {
	return Notify(Notification{
		Title:   "Forge Cloud",
		Message: "Connection restored",
		Level:   NotifySuccess,
		Sound:   false,
	})
}

// ---------------------------------------------------------------------------
// Terminal notifications (for when desktop isn't available)
// ---------------------------------------------------------------------------

// TerminalNotify prints a notification to the terminal with formatting.
func TerminalNotify(n Notification) {
	// ANSI colors
	var color string
	switch n.Level {
	case NotifySuccess:
		color = "\033[32m" // green
	case NotifyWarning:
		color = "\033[33m" // yellow
	case NotifyError:
		color = "\033[31m" // red
	default:
		color = "\033[36m" // cyan
	}
	reset := "\033[0m"

	// Icon
	icon := "ℹ"
	switch n.Level {
	case NotifySuccess:
		icon = "✓"
	case NotifyWarning:
		icon = "⚠"
	case NotifyError:
		icon = "✗"
	}

	// Bell if sound enabled
	bell := ""
	if n.Sound {
		bell = "\a"
	}

	fmt.Fprintf(os.Stderr, "%s%s%s %s%s%s %s\n",
		bell, color, icon, n.Title, reset, ":", n.Message)
}

// ---------------------------------------------------------------------------
// Notification preferences
// ---------------------------------------------------------------------------

// NotificationPrefs holds user notification preferences.
type NotificationPrefs struct {
	Enabled       bool `json:"enabled"`
	Desktop       bool `json:"desktop"`
	Sound         bool `json:"sound"`
	TaskAssigned  bool `json:"taskAssigned"`
	TaskCompleted bool `json:"taskCompleted"`
	ReviewFeedback bool `json:"reviewFeedback"`
	ConnectionEvents bool `json:"connectionEvents"`
}

// DefaultNotificationPrefs returns sensible defaults.
func DefaultNotificationPrefs() NotificationPrefs {
	return NotificationPrefs{
		Enabled:          true,
		Desktop:          true,
		Sound:            true,
		TaskAssigned:     true,
		TaskCompleted:    true,
		ReviewFeedback:   true,
		ConnectionEvents: false, // these can be noisy
	}
}

// notificationPrefs is the global preferences, loaded once.
var notificationPrefs *NotificationPrefs

// GetNotificationPrefs returns the current notification preferences.
func GetNotificationPrefs() NotificationPrefs {
	if notificationPrefs != nil {
		return *notificationPrefs
	}
	// TODO: load from ~/.forge/config.json
	prefs := DefaultNotificationPrefs()
	notificationPrefs = &prefs
	return prefs
}

// ShouldNotify checks if a notification should be sent based on preferences.
func ShouldNotify(category string) bool {
	prefs := GetNotificationPrefs()
	if !prefs.Enabled {
		return false
	}
	switch strings.ToLower(category) {
	case "task_assigned":
		return prefs.TaskAssigned
	case "task_completed":
		return prefs.TaskCompleted
	case "review_feedback":
		return prefs.ReviewFeedback
	case "connection":
		return prefs.ConnectionEvents
	default:
		return true
	}
}
