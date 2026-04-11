package forge

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// UpdateTaskMetadata updates one or more metadata comments for a specific task
// in a spec file. It finds the "### Task: {taskName}" section and replaces
// or inserts the specified metadata comments.
// specWriteGlobalMu protects all spec file writes from concurrent access.
// This is a package-level lock because UpdateTaskMetadata is called as a
// standalone function (not a Service method) from multiple goroutines in
// the watch daemon and the live executor.
var specWriteGlobalMu sync.Mutex

func UpdateTaskMetadata(specPath, taskName string, updates map[string]string) error {
	specWriteGlobalMu.Lock()
	defer specWriteGlobalMu.Unlock()
	data, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")

	result, err := updateMetadataInLines(lines, taskName, updates)
	if err != nil {
		return fmt.Errorf("updating %s in %s: %w", taskName, specPath, err)
	}

	output := strings.Join(result, "\n")
	return os.WriteFile(specPath, []byte(output), 0o644)
}

// updateMetadataInLines is the pure logic function that operates on string slices.
func updateMetadataInLines(lines []string, taskName string, updates map[string]string) ([]string, error) {
	if len(updates) == 0 {
		return lines, nil
	}

	// Find the task section boundaries.
	taskStart := -1
	taskEnd := len(lines)
	targetPrefix := "### Task:"

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, targetPrefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, targetPrefix))
		if !strings.EqualFold(name, taskName) {
			if taskStart >= 0 {
				taskEnd = i
				break
			}
			continue
		}
		taskStart = i
	}

	if taskStart < 0 {
		return nil, fmt.Errorf("task %q not found", taskName)
	}

	// Find the end of the task section (next ### Task: or ## heading or EOF).
	for i := taskStart + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "### Task:") ||
			(strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ")) {
			taskEnd = i
			break
		}
	}

	// Apply updates within the task section.
	applied := map[string]bool{}
	lastMetadataLine := taskStart // track last metadata comment position

	result := make([]string, 0, len(lines))
	result = append(result, lines[:taskStart+1]...)

	for i := taskStart + 1; i < taskEnd; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if key, _, ok := parseMetadataComment(trimmed); ok {
			lastMetadataLine = len(result)
			if newValue, wantUpdate := updates[key]; wantUpdate {
				result = append(result, fmt.Sprintf("<!-- %s: %s -->", key, newValue))
				applied[key] = true
				continue
			}
		}

		result = append(result, line)
	}

	// Insert any updates that didn't match an existing comment.
	// Insert them after the last metadata comment in the section.
	var toInsert []string
	for key, value := range updates {
		if !applied[key] {
			toInsert = append(toInsert, fmt.Sprintf("<!-- %s: %s -->", key, value))
		}
	}

	if len(toInsert) > 0 {
		insertAt := lastMetadataLine + 1
		if insertAt > len(result) {
			insertAt = len(result)
		}
		// Sort for deterministic output.
		sortedInsert := make([]string, len(toInsert))
		copy(sortedInsert, toInsert)
		sortStrings(sortedInsert)

		expanded := make([]string, 0, len(result)+len(sortedInsert)+len(lines)-taskEnd)
		expanded = append(expanded, result[:insertAt]...)
		expanded = append(expanded, sortedInsert...)
		expanded = append(expanded, result[insertAt:]...)
		result = expanded
	}

	result = append(result, lines[taskEnd:]...)
	return result, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
