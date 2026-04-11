package forge

import (
	"fmt"
	"os"
	"strings"
)

// ProposedMemory is a single entry parsed from inbox.md under the
// "## Proposed memory" section. Agents write these; humans review them
// and promote to kb/memory.md.
type ProposedMemory struct {
	LineNumber int    `json:"lineNumber"`
	Raw        string `json:"raw"`
	Text       string `json:"text"`
}

// ListProposedMemories reads inbox.md and returns all proposed memory
// entries under the "## Proposed memory" section.
func (s *Service) ListProposedMemories(cwd string) ([]ProposedMemory, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return nil, err
	}
	return loadProposedMemoriesFromFile(ctx.InboxFile)
}

// AppendProposedMemory adds a new entry to inbox.md under the
// "## Proposed memory" section. If the section doesn't exist yet,
// it is created at the end of the file. Used by the runtime protocol
// when an agent emits a "proposed_memory" event.
func (s *Service) AppendProposedMemory(cwd, entry string) error {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return err
	}
	return appendProposedMemoryToFile(ctx.InboxFile, entry)
}

func loadProposedMemoriesFromFile(path string) ([]ProposedMemory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	inSection := false
	var memories []ProposedMemory

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			if strings.EqualFold(trimmed, "## Proposed memory") {
				inSection = true
				continue
			}
			// Another section starts — close the proposed memory section.
			if inSection {
				break
			}
		}

		if !inSection {
			continue
		}

		// A bullet line under the section is a proposed memory entry.
		if strings.HasPrefix(trimmed, "- ") {
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if text == "" {
				continue
			}
			memories = append(memories, ProposedMemory{
				LineNumber: i + 1,
				Raw:        line,
				Text:       text,
			})
		}
	}

	return memories, nil
}

func appendProposedMemoryToFile(path, entry string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	content := string(data)
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return fmt.Errorf("empty entry")
	}

	// Ensure the section exists.
	if !strings.Contains(content, "## Proposed memory") {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n## Proposed memory\n\nAgents propose additions to `kb/memory.md` here.\n"
	}

	// Find the section and insert the new entry at the end.
	lines := strings.Split(content, "\n")
	sectionStart := -1
	sectionEnd := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "## Proposed memory") {
			sectionStart = i
			continue
		}
		if sectionStart >= 0 && strings.HasPrefix(trimmed, "## ") {
			sectionEnd = i
			break
		}
	}

	if sectionStart < 0 {
		return fmt.Errorf("proposed memory section not found after insertion")
	}

	// Walk back from sectionEnd to skip trailing blank lines.
	insertAt := sectionEnd
	for insertAt > sectionStart+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}

	newLine := "- " + entry
	updated := append([]string(nil), lines[:insertAt]...)
	updated = append(updated, newLine)
	updated = append(updated, lines[insertAt:]...)

	return os.WriteFile(path, []byte(strings.Join(updated, "\n")), 0o644)
}
