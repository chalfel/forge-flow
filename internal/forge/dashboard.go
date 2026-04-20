package forge

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Milestone is a section of the roadmap (Now / Next / Later) with its bullet
// items and computed progress.
type Milestone struct {
	Name     string          `json:"name"`
	Bullets  []MilestoneItem `json:"bullets"`
	DoneCount int            `json:"doneCount"`
	Total    int             `json:"total"`
	DonePct  int             `json:"donePct"`
	Current  bool            `json:"current"`
}

// MilestoneItem is a single roadmap bullet. Matched to a spec when possible so
// the dashboard can correlate roadmap intent with actual spec progress.
type MilestoneItem struct {
	Text        string `json:"text"`
	Done        bool   `json:"done"`
	MatchedSpec string `json:"matchedSpec,omitempty"`
	SpecStatus  string `json:"specStatus,omitempty"`
	SpecProgress int   `json:"specProgress,omitempty"` // 0-100
}

// DashboardView is the full state for the /dashboard page.
type DashboardView struct {
	ProjectName    string            `json:"projectName"`
	Milestones     []Milestone       `json:"milestones"`
	HasRoadmap     bool              `json:"hasRoadmap"`
	CachedAnalysis *ProjectAnalysis  `json:"cachedAnalysis,omitempty"`
	TotalSpecs     int               `json:"totalSpecs"`
	DoneSpecs      int               `json:"doneSpecs"`
	SpecSummaries  []SpecSummary     `json:"specSummaries,omitempty"`
}

// SpecSummary is a compact spec state used by the dashboard prompt composer
// and as context for Claude.
type SpecSummary struct {
	File       string `json:"file"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	DoneTasks  int    `json:"doneTasks"`
	TotalTasks int    `json:"totalTasks"`
}

// Dashboard loads the dashboard state for the current project: parses the
// roadmap, correlates bullets with specs, and attaches the cached analysis.
func (s *Service) Dashboard(cwd string) (DashboardView, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return DashboardView{}, err
	}

	view := DashboardView{
		ProjectName: ctx.ProjectName,
	}

	// Load specs for correlation.
	specs, err := s.inspectSpecsForDashboard(ctx)
	if err == nil {
		view.SpecSummaries = specs
		for _, sp := range specs {
			view.TotalSpecs++
			if sp.Status == "done" || (sp.TotalTasks > 0 && sp.DoneTasks == sp.TotalTasks) {
				view.DoneSpecs++
			}
		}
	}

	// Load the roadmap and parse milestones.
	roadmapPath := filepath.Join(ctx.KBDir, "roadmap.md")
	if data, err := os.ReadFile(roadmapPath); err == nil {
		milestones := parseRoadmapMilestones(string(data))
		matchBulletsToSpecs(milestones, specs)
		computeMilestoneProgress(milestones)
		markCurrentMilestone(milestones)
		view.Milestones = milestones
		view.HasRoadmap = len(milestones) > 0
	}

	// Attach cached analysis if present.
	if cached, err := s.LoadCachedAnalysis(cwd); err == nil && cached != nil {
		view.CachedAnalysis = cached
	}

	return view, nil
}

// inspectSpecsForDashboard parses every spec in the project and returns a
// compact summary suitable for the dashboard view and prompt composition.
func (s *Service) inspectSpecsForDashboard(ctx ResolvedContext) ([]SpecSummary, error) {
	entries, err := os.ReadDir(ctx.SpecsDir)
	if err != nil {
		return nil, err
	}

	var out []SpecSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		spec, err := parseForgeSpecFile(filepath.Join(ctx.SpecsDir, entry.Name()), ctx.SpecsDir)
		if err != nil {
			continue
		}
		sum := SpecSummary{
			File:   entry.Name(),
			Title:  spec.Title,
			Status: spec.Status,
		}
		for _, t := range spec.Tasks {
			sum.TotalTasks++
			if t.Status == "done" {
				sum.DoneTasks++
			}
		}
		out = append(out, sum)
	}
	return out, nil
}

// Section header regex: matches `## Now`, `## Next`, `## Later` case-insensitive
// with optional trailing parenthetical like `## Now (shipping)`.
var milestoneHeadingRe = regexp.MustCompile(`(?i)^##\s+(now|next|later)\b`)

// Bullet regex: matches `- item text` at the top level only (no leading
// whitespace). Nested bullets (two or more leading spaces) are treated as
// details of the parent item and skipped.
var bulletRe = regexp.MustCompile(`^-\s+(.+?)\s*$`)

// Detect `(done ...)` or `(shipped ...)` substring marking a bullet as complete.
var doneTokenRe = regexp.MustCompile(`(?i)\((?:done|shipped)[^)]*\)`)

// parseRoadmapMilestones extracts `## Now`, `## Next`, `## Later` sections
// with their bullet items from a roadmap markdown document.
func parseRoadmapMilestones(content string) []Milestone {
	var milestones []Milestone
	var current *Milestone

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if m := milestoneHeadingRe.FindStringSubmatch(line); m != nil {
			name := strings.Title(strings.ToLower(m[1]))
			milestones = append(milestones, Milestone{Name: name})
			current = &milestones[len(milestones)-1]
			continue
		}
		// Another ## heading ends the current section.
		if strings.HasPrefix(line, "## ") && current != nil {
			current = nil
			continue
		}
		if current == nil {
			continue
		}
		if b := bulletRe.FindStringSubmatch(line); b != nil {
			text := strings.TrimSpace(b[1])
			if text == "" {
				continue
			}
			done := doneTokenRe.MatchString(text)
			current.Bullets = append(current.Bullets, MilestoneItem{
				Text: text,
				Done: done,
			})
		}
	}
	return milestones
}

// matchBulletsToSpecs tries to match each bullet to a spec by fuzzy title
// comparison. If a match is found and the spec is done (or progress >= 100),
// the bullet is marked done.
func matchBulletsToSpecs(milestones []Milestone, specs []SpecSummary) {
	for mi := range milestones {
		for bi := range milestones[mi].Bullets {
			b := &milestones[mi].Bullets[bi]
			matched := bestSpecMatch(b.Text, specs)
			if matched == nil {
				continue
			}
			b.MatchedSpec = matched.File
			b.SpecStatus = matched.Status
			if matched.TotalTasks > 0 {
				b.SpecProgress = matched.DoneTasks * 100 / matched.TotalTasks
			}
			if matched.Status == "done" || (matched.TotalTasks > 0 && matched.DoneTasks == matched.TotalTasks) {
				b.Done = true
			}
		}
	}
}

// bestSpecMatch finds the spec whose title best matches the bullet's title
// phrase (the text before an em-dash or double-hyphen). Uses Jaccard
// similarity to avoid false positives from common shared words like "board"
// or "from". Returns nil if no spec crosses the 0.4 similarity threshold.
func bestSpecMatch(bullet string, specs []SpecSummary) *SpecSummary {
	bulletTitle := extractBulletTitle(bullet)
	bulletTitle = stripMarkdownEmphasis(strings.ToLower(bulletTitle))
	bulletWords := wordSet(bulletTitle)
	if len(bulletWords) < 2 {
		return nil
	}

	var best *SpecSummary
	bestScore := 0.4 // Jaccard threshold
	for i := range specs {
		specTitle := extractBulletTitle(specs[i].Title)
		specTitle = strings.ToLower(specTitle)
		specWords := wordSet(specTitle)
		if len(specWords) < 2 {
			continue
		}
		score := jaccard(bulletWords, specWords)
		if score > bestScore {
			bestScore = score
			best = &specs[i]
		}
	}
	return best
}

// extractBulletTitle returns the first clause of a bullet or spec title,
// stripping off everything after an em-dash, double-hyphen, or colon. This
// gives a cleaner signal for fuzzy matching since descriptions tend to add
// noise words.
func extractBulletTitle(s string) string {
	for _, sep := range []string{" — ", " -- ", ": "} {
		if i := strings.Index(s, sep); i >= 0 {
			return s[:i]
		}
	}
	return s
}

// jaccard returns |A∩B| / |A∪B| for two word sets.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// stripMarkdownEmphasis removes **bold** and *italic* markers for cleaner
// word matching.
func stripMarkdownEmphasis(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

// wordSet returns a set of meaningful lowercase words (length >= 4) from a
// string. Short words filter out common noise like "the", "and", "is".
func wordSet(s string) map[string]bool {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !isWordRune(r)
	})
	set := map[string]bool{}
	for _, w := range words {
		if len(w) >= 4 {
			set[w] = true
		}
	}
	return set
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func intersectCount(a, b map[string]bool) int {
	count := 0
	for w := range a {
		if b[w] {
			count++
		}
	}
	return count
}

// computeMilestoneProgress fills DoneCount, Total, and DonePct for each
// milestone.
func computeMilestoneProgress(milestones []Milestone) {
	for i := range milestones {
		m := &milestones[i]
		m.Total = len(m.Bullets)
		m.DoneCount = 0
		for _, b := range m.Bullets {
			if b.Done {
				m.DoneCount++
			}
		}
		if m.Total > 0 {
			m.DonePct = m.DoneCount * 100 / m.Total
		}
	}
}

// markCurrentMilestone marks the first milestone that has pending work as
// Current. If all are fully done, the last milestone is marked current.
func markCurrentMilestone(milestones []Milestone) {
	for i := range milestones {
		if milestones[i].DonePct < 100 && len(milestones[i].Bullets) > 0 {
			milestones[i].Current = true
			return
		}
	}
	if len(milestones) > 0 {
		milestones[len(milestones)-1].Current = true
	}
}
