package forge

import (
	"fmt"
	"html/template"
	"math"
	"strings"
)

// SpecDetailView holds all data for the spec detail page.
type SpecDetailView struct {
	Spec        ForgeSpecFile `json:"spec"`
	ProjectName string        `json:"projectName"`
	TotalTasks  int           `json:"totalTasks"`
	DoneTasks   int           `json:"doneTasks"`
	ProgressPct float64       `json:"progressPct"`
	Lane        string        `json:"lane"`
}

// SpecDetail loads a single spec for the detail view.
func (s *Service) SpecDetail(cwd, specFile string) (SpecDetailView, error) {
	ctx, err := s.ResolveContext(cwd)
	if err != nil {
		return SpecDetailView{}, err
	}

	specPath, err := safeJoin(ctx.SpecsDir, specFile)
	if err != nil {
		return SpecDetailView{}, err
	}
	spec, err := parseForgeSpecFile(specPath, ctx.SpecsDir)
	if err != nil {
		return SpecDetailView{}, fmt.Errorf("parsing spec %s: %w", specFile, err)
	}

	total := len(spec.Tasks)
	done := 0
	active := 0
	ready := 0
	blocked := 0
	for _, t := range spec.Tasks {
		switch t.Status {
		case "done":
			done++
		case "in_progress", "in_review":
			active++
		case "todo", "changes_requested":
			// Check deps
			allMet := true
			for _, dep := range t.Deps {
				for _, other := range spec.Tasks {
					if strings.EqualFold(strings.TrimSpace(dep), other.Name) && other.Status != "done" {
						allMet = false
					}
				}
			}
			if allMet {
				ready++
			} else {
				blocked++
			}
		}
	}

	pct := 0.0
	if total > 0 {
		pct = math.Round(float64(done) / float64(total) * 100)
	}

	lane := "Ready"
	if done == total && total > 0 {
		lane = "Done"
	} else if active > 0 {
		lane = "In Progress"
	} else if blocked > 0 && ready == 0 {
		lane = "Blocked"
	}

	return SpecDetailView{
		Spec:        spec,
		ProjectName: ctx.ProjectName,
		TotalTasks:  total,
		DoneTasks:   done,
		ProgressPct: pct,
		Lane:        lane,
	}, nil
}

var specDetailTemplate = template.Must(template.New("spec-detail").Funcs(funcMap).Parse(specDetailHTML))

const specDetailHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Spec.Title}} — Forge</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <script>
    tailwind.config = {
      theme: {
        extend: {
          fontFamily: { sans: ['Inter', 'system-ui', 'sans-serif'], mono: ['JetBrains Mono', 'monospace'] },
          colors: {
            surface: { 0: '#0a0a0a', 1: '#111111', 2: '#191919', 3: '#222222', 4: '#2a2a2a' },
            border: { subtle: 'rgba(255,255,255,0.06)', medium: 'rgba(255,255,255,0.1)' },
            txt: { primary: 'rgba(255,255,255,0.92)', secondary: 'rgba(255,255,255,0.55)', tertiary: 'rgba(255,255,255,0.35)' },
            accent: { purple: '#8b5cf6', blue: '#3b82f6', amber: '#f59e0b', red: '#ef4444', green: '#22c55e' }
          }
        }
      }
    }
  </script>
  <style>
    body { background: #0a0a0a; color: rgba(255,255,255,0.92); }
    * { scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.1) transparent; }
  </style>
</head>
<body class="min-h-screen font-sans antialiased">

<header class="border-b border-border-subtle">
  <div class="max-w-4xl mx-auto px-6 h-14 flex items-center gap-3">
    <a href="/" class="flex items-center gap-3 hover:opacity-80 transition-opacity">
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
        <rect width="20" height="20" rx="5" fill="#8b5cf6"/>
        <path d="M6 10h8M10 6v8" stroke="white" stroke-width="1.5" stroke-linecap="round"/>
      </svg>
      <span class="text-[15px] font-semibold tracking-tight">Forge</span>
    </a>
    <span class="text-txt-tertiary">/</span>
    <a href="/" class="text-[15px] text-txt-secondary hover:text-txt-primary transition-colors">{{.ProjectName}}</a>
    <span class="text-txt-tertiary">/</span>
    <span class="text-[15px] text-txt-primary">Spec</span>
  </div>
</header>

<main class="max-w-4xl mx-auto px-6 py-8">

  <div class="flex items-start justify-between gap-4 mb-6">
    <div class="flex-1">
      <h1 class="text-2xl font-semibold tracking-tight leading-tight">{{.Spec.Title}}</h1>
      <div class="flex items-center gap-3 mt-3">
        {{if .Spec.Priority}}<span class="text-[12px] font-mono px-2 py-0.5 rounded border border-border-subtle text-txt-secondary">{{.Spec.Priority}}</span>{{end}}
        {{if .Spec.Created}}<span class="text-[12px] font-mono text-txt-tertiary">{{.Spec.Created}}</span>{{end}}
        {{if .Spec.Branch}}<span class="text-[12px] font-mono text-accent-purple">{{.Spec.Branch}}</span>{{end}}
      </div>
    </div>
    <div class="text-right flex-shrink-0">
      <div class="text-3xl font-semibold" style="color: {{laneAccent .Lane}}">{{progressWidth .ProgressPct}}</div>
      <div class="text-[12px] text-txt-tertiary mt-1">{{.DoneTasks}}/{{.TotalTasks}} tasks done</div>
    </div>
  </div>

  <div class="h-1.5 bg-surface-3 rounded-full overflow-hidden mb-8">
    <div class="h-full rounded-full transition-all duration-500" style="width: {{progressWidth .ProgressPct}}; background: {{laneAccent .Lane}}"></div>
  </div>

  {{if .Spec.Demo}}
  <section class="mb-8">
    <h2 class="text-[13px] font-semibold uppercase tracking-wider text-txt-tertiary mb-3">Demo</h2>
    <div class="bg-surface-1 border border-border-subtle rounded-lg px-4 py-3">
      <p class="text-[14px] text-txt-secondary leading-relaxed">{{.Spec.Demo}}</p>
    </div>
  </section>
  {{end}}

  {{if .Spec.ExpectedBehavior}}
  <section class="mb-8">
    <h2 class="text-[13px] font-semibold uppercase tracking-wider text-txt-tertiary mb-3">Expected Behavior</h2>
    <div class="space-y-1.5">
      {{range .Spec.ExpectedBehavior}}
      <div class="flex items-start gap-2 text-[14px] text-txt-secondary">
        <span class="text-txt-tertiary mt-0.5">-</span>
        <span>{{.}}</span>
      </div>
      {{end}}
    </div>
  </section>
  {{end}}

  {{if .Spec.TestCases}}
  <section class="mb-8">
    <h2 class="text-[13px] font-semibold uppercase tracking-wider text-txt-tertiary mb-3">Test Cases</h2>
    <div class="space-y-3">
      {{range .Spec.TestCases}}
      <div class="bg-surface-1 border border-border-subtle rounded-lg px-4 py-3">
        <h3 class="text-[14px] font-medium text-txt-primary mb-2">{{.Name}}</h3>
        {{range .Steps}}
        <div class="flex items-start gap-2 text-[13px] ml-2 mb-1">
          <span class="font-mono text-accent-purple font-medium min-w-[50px]">{{.Keyword}}</span>
          <span class="text-txt-secondary">{{.Text}}</span>
        </div>
        {{end}}
      </div>
      {{end}}
    </div>
  </section>
  {{end}}

  {{if .Spec.ValidationPlan}}
  <section class="mb-8">
    <h2 class="text-[13px] font-semibold uppercase tracking-wider text-txt-tertiary mb-3">Validation Plan</h2>
    <div class="space-y-1.5">
      {{range .Spec.ValidationPlan}}
      <div class="flex items-start gap-2 text-[14px] text-txt-secondary">
        <span class="text-txt-tertiary mt-0.5">-</span>
        <span>{{.}}</span>
      </div>
      {{end}}
    </div>
  </section>
  {{end}}

  <section class="mb-8">
    <h2 class="text-[13px] font-semibold uppercase tracking-wider text-txt-tertiary mb-3">Tasks</h2>
    <div class="space-y-2">
      {{range .Spec.Tasks}}
      <div class="bg-surface-1 border border-border-subtle rounded-lg px-4 py-4">
        <div class="flex items-center gap-3 mb-3">
          {{taskDot .Status}}
          <h3 class="text-[14px] font-medium text-txt-primary flex-1">{{.Name}}</h3>
          {{if .Repo}}<span class="text-[11px] font-mono text-txt-tertiary border border-border-subtle rounded px-1.5 py-0.5">{{.Repo}}</span>{{end}}
          {{if .PR}}<a href="{{.PR}}" target="_blank" class="text-[11px] font-mono text-accent-purple hover:underline">PR</a>{{end}}
        </div>

        <div class="flex flex-wrap gap-2 mb-3 text-[11px] font-mono">
          <span class="px-1.5 py-0.5 rounded bg-surface-3 text-txt-tertiary">{{.Status}}</span>
          {{if .Parallelizable}}<span class="px-1.5 py-0.5 rounded bg-surface-3 text-txt-tertiary">{{if eq .Parallelizable "yes"}}parallel{{else}}serial{{end}}</span>{{end}}
          {{if .ConflictRisk}}<span class="px-1.5 py-0.5 rounded bg-surface-3 text-txt-tertiary">risk: {{.ConflictRisk}}</span>{{end}}
        </div>

        {{if .Deps}}
        <div class="mb-2">
          <span class="text-[11px] font-mono text-txt-tertiary">deps:</span>
          {{range .Deps}}<span class="text-[11px] font-mono text-txt-secondary ml-1">{{.}}</span>{{end}}
        </div>
        {{end}}

        {{if .Touches}}
        <div class="mb-2">
          <span class="text-[11px] font-mono text-txt-tertiary">touches:</span>
          {{range .Touches}}<span class="text-[11px] font-mono text-txt-secondary ml-1">{{.}}</span>{{end}}
        </div>
        {{end}}

        {{if .DoneWhen}}
        <div class="mt-3 border-t border-border-subtle pt-3">
          <p class="text-[11px] font-semibold uppercase tracking-wider text-txt-tertiary mb-1.5">Done when</p>
          {{range .DoneWhen}}
          <div class="flex items-start gap-2 text-[13px] text-txt-secondary mb-1">
            <span class="text-txt-tertiary mt-0.5">-</span>
            <span>{{.}}</span>
          </div>
          {{end}}
        </div>
        {{end}}

        {{if .Validation}}
        <div class="mt-3 border-t border-border-subtle pt-3">
          <p class="text-[11px] font-semibold uppercase tracking-wider text-txt-tertiary mb-1.5">Validation</p>
          {{range .Validation}}
          <div class="flex items-start gap-2 text-[12px] text-txt-tertiary mb-1">
            <span class="mt-0.5">-</span>
            <span>{{.}}</span>
          </div>
          {{end}}
        </div>
        {{end}}
      </div>
      {{end}}
    </div>
  </section>

</main>
</body>
</html>`
