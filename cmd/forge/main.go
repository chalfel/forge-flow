package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chalfel/forge-flow/internal/forge"
	"github.com/chalfel/forge-flow/internal/forge/cloud"
	"github.com/charmbracelet/huh"
)

func main() {
	svc, err := forge.NewServiceFromEnv()
	if err != nil {
		fatal(err)
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "workspace":
		handleWorkspace(svc, os.Args[2:])
	case "project":
		handleProject(svc, os.Args[2:])
	case "repo":
		handleRepo(svc, os.Args[2:])
	case "spec":
		handleSpec(svc, os.Args[2:])
	case "test":
		handleTest(svc, os.Args[2:])
	case "run":
		handleRun(svc, os.Args[2:])
	case "board":
		handleBoard(svc, os.Args[2:])
	case "history":
		handleHistory(svc, os.Args[2:])
	case "memory":
		handleMemory(svc, os.Args[2:])
	case "events":
		handleEvents(svc, os.Args[2:])
	case "decisions":
		handleDecisions(svc, os.Args[2:])
	case "mcp":
		handleMCP(svc, os.Args[2:])
	case "context":
		handleContext(svc, os.Args[2:])
	case "dev":
		handleDev(svc, os.Args[2:])
	case "cloud":
		handleCloud(svc, os.Args[2:])
	case "login":
		handleCloud(svc, []string{"login"})
	case "task":
		handleTask(svc, os.Args[2:])
	case "daemon":
		handleDaemon(os.Args[2:])
	case "pi":
		handlePI(svc, os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fatal(fmt.Errorf("unknown command: %s", os.Args[1]))
	}
}

func handleWorkspace(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing workspace subcommand"))
	}
	switch args[0] {
	case "init":
		fs := flag.NewFlagSet("forge workspace init", flag.ExitOnError)
		name := fs.String("name", "main", "workspace id")
		displayName := fs.String("display-name", "", "human readable workspace name")
		root := fs.String("path", "", "workspace root path (defaults to ~/.forge/workspaces/<name>)")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		result, err := svc.InitWorkspace(*name, *displayName, *root)
		if err != nil {
			fatal(err)
		}
		printResult(*asJSON, result, fmt.Sprintf("Workspace %s ready at %s", result.WorkspaceID, result.Root))
	case "list":
		fs := flag.NewFlagSet("forge workspace list", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		items, err := svc.ListWorkspaces()
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(items)
			return
		}
		for _, item := range items {
			fmt.Printf("%s\t%s\t%s\n", item.WorkspaceID, item.Name, item.Root)
		}
	default:
		fatal(fmt.Errorf("unknown workspace subcommand: %s", args[0]))
	}
}

func handleProject(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing project subcommand"))
	}
	switch args[0] {
	case "init":
		fs := flag.NewFlagSet("forge project init", flag.ExitOnError)
		workspace := fs.String("workspace", "main", "workspace id")
		projectID := fs.String("project-id", "", "stable project id")
		name := fs.String("name", "", "project name")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		result, err := svc.InitProject(*workspace, *projectID, *name)
		if err != nil {
			fatal(err)
		}
		printResult(*asJSON, result, fmt.Sprintf("Project %s ready at %s", result.ProjectID, result.ProjectRoot))
	case "list":
		fs := flag.NewFlagSet("forge project list", flag.ExitOnError)
		workspace := fs.String("workspace", "main", "workspace id")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		items, err := svc.ListProjects(*workspace)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(items)
			return
		}
		for _, item := range items {
			fmt.Printf("%s\t%s\t%s\n", item.ProjectID, item.ProjectName, item.ProjectRoot)
		}
	case "create":
		fs := flag.NewFlagSet("forge project create", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve creation paths from")
		workspace := fs.String("workspace", "main", "workspace id")
		projectID := fs.String("project-id", "", "stable project id")
		name := fs.String("name", "", "project name")
		pack := fs.String("pack", "blank", "stack pack id")
		packsDir := fs.String("packs-dir", "", "path list of local stack-pack roots to search before workspace/built-in packs")
		topology := fs.String("topology", "", "project topology: single-repo|monorepo|multi-repo")
		pathFlag := fs.String("path", "", "target repo path or multi-repo parent directory")
		rootRepoID := fs.String("root-repo-id", "", "override root repo id for single-repo or monorepo packs")
		listPacks := fs.Bool("list-packs", false, "list available stack packs")
		existing := fs.Bool("existing", false, "link existing repos instead of creating from a pack")
		repos := fs.String("repos", "", "comma-separated repo paths for --existing mode (skips interactive selection)")
		noDevSetup := fs.Bool("no-dev-setup", false, "skip dev session config generation (--existing only)")
		apply := fs.Bool("apply", false, "apply creation instead of plan-only output")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])

		if *existing {
			handleProjectCreateExisting(svc, *cwd, *workspace, *projectID, *name, *pathFlag, *repos, *apply, *asJSON, !*noDevSetup)
			return
		}

		configureStackPackSources(*packsDir)
		if *listPacks {
			items, err := svc.ListStackPacks()
			if err != nil {
				fatal(err)
			}
			if *asJSON {
				printJSON(items)
				return
			}
			for _, item := range items {
				source := fallbackString(item.SourceKind, "builtin")
				if item.SourcePath != "" {
					source = fmt.Sprintf("%s:%s", source, item.SourcePath)
				} else if item.SourceName != "" {
					source = fmt.Sprintf("%s:%s", source, item.SourceName)
				}
				fmt.Printf("%s\t%s\t%s\t[%s]\n", item.ID, item.Name, item.Description, source)
			}
			return
		}
		input := forge.ProjectCreateInput{CWD: *cwd, Workspace: *workspace, ProjectID: *projectID, ProjectName: *name, Pack: *pack, Topology: *topology, Path: *pathFlag, RootRepoID: *rootRepoID}
		var (
			plan forge.ProjectCreatePlan
			err  error
		)
		if *apply {
			plan, err = svc.ApplyProjectCreate(input)
		} else {
			plan, err = svc.PlanProjectCreate(input)
		}
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(plan)
			return
		}
		fmt.Print(formatProjectCreatePlan(plan))
	case "add":
		parseArgs := append([]string(nil), args[1:]...)
		positionalKind := ""
		if len(parseArgs) > 0 && !strings.HasPrefix(parseArgs[0], "-") {
			positionalKind = parseArgs[0]
			parseArgs = parseArgs[1:]
		}
		fs := flag.NewFlagSet("forge project add", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		workspace := fs.String("workspace", "", "workspace id (optional when cwd is already linked)")
		projectID := fs.String("project-id", "", "project id (optional when cwd is already linked)")
		kind := fs.String("kind", "", "component kind to add (web, api, mobile, worker, admin, package, docs, ...)")
		componentID := fs.String("component-id", "", "stable logical component id")
		name := fs.String("name", "", "human-readable component name")
		stack := fs.String("stack", "", "optional stack label")
		as := fs.String("as", "", "host the component as repo|app|package|module")
		repoID := fs.String("repo-id", "", "repo id when adding a repo-backed component")
		repoName := fs.String("repo-name", "", "repo name when adding a repo-backed component")
		repoRole := fs.String("repo-role", "", "role for the new repo or component")
		pathFlag := fs.String("path", "", "target repo path or relative app/package/module path")
		apply := fs.Bool("apply", false, "apply metadata/filesystem changes instead of plan-only output")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(parseArgs)
		remaining := fs.Args()
		if *kind == "" && positionalKind != "" {
			*kind = positionalKind
		} else if *kind == "" && len(remaining) > 0 {
			*kind = remaining[0]
		}
		if strings.TrimSpace(*kind) == "" {
			fatal(fmt.Errorf("forge project add requires --kind <value> or a positional kind argument"))
		}
		input := forge.ProjectAddInput{CWD: *cwd, Workspace: *workspace, ProjectID: *projectID, Kind: *kind, ComponentID: *componentID, Name: *name, Stack: *stack, As: *as, RepoID: *repoID, RepoName: *repoName, RepoRole: *repoRole, Path: *pathFlag}
		var (
			plan forge.ProjectAddPlan
			err  error
		)
		if *apply {
			plan, err = svc.ApplyProjectAdd(input)
		} else {
			plan, err = svc.PlanProjectAdd(input)
		}
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(plan)
			return
		}
		fmt.Print(formatProjectAddPlan(plan))
	default:
		fatal(fmt.Errorf("unknown project subcommand: %s", args[0]))
	}
}

func handleRepo(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing repo subcommand"))
	}
	switch args[0] {
	case "link":
		fs := flag.NewFlagSet("forge repo link", flag.ExitOnError)
		workspace := fs.String("workspace", "main", "workspace id")
		projectID := fs.String("project-id", "", "project id")
		repoID := fs.String("repo-id", "", "repo id used in specs")
		repoName := fs.String("repo-name", "", "human readable repo name")
		role := fs.String("role", "", "repo role")
		gitRemote := fs.String("git", "", "git remote url")
		pathFlag := fs.String("path", "", "repo path to link (defaults to current repo root)")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		result, err := svc.LinkRepo(*workspace, *projectID, *repoID, *repoName, *role, *gitRemote, *pathFlag)
		if err != nil {
			fatal(err)
		}
		printResult(*asJSON, result, fmt.Sprintf("Repo %s linked to project %s", result.RepoID, result.ProjectID))
	case "list":
		fs := flag.NewFlagSet("forge repo list", flag.ExitOnError)
		workspace := fs.String("workspace", "main", "workspace id")
		projectID := fs.String("project-id", "", "project id")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		items, err := svc.ListRepos(*workspace, *projectID)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(items)
			return
		}
		for _, item := range items {
			fmt.Printf("%s\t%s\t%s\t%s\n", item.RepoID, item.Name, item.Role, item.LocalPath)
		}
	default:
		fatal(fmt.Errorf("unknown repo subcommand: %s", args[0]))
	}
}

func handleContext(svc *forge.Service, args []string) {
	if len(args) == 0 || args[0] != "resolve" {
		fatal(fmt.Errorf("use: forge context resolve"))
	}
	fs := flag.NewFlagSet("forge context resolve", flag.ExitOnError)
	cwd := fs.String("cwd", "", "path to resolve from")
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args[1:])
	result, err := svc.ResolveContext(*cwd)
	if err != nil {
		fatal(err)
	}
	if *asJSON {
		printJSON(result)
		return
	}
	fmt.Printf("workspace: %s\n", result.Workspace)
	fmt.Printf("project:   %s (%s)\n", result.ProjectName, result.ProjectID)
	fmt.Printf("project root: %s\n", result.ProjectRoot)
	fmt.Printf("repo:      %s\n", result.RepoID)
	if result.RepoRoot != "" {
		fmt.Printf("repo root: %s\n", result.RepoRoot)
	}
	fmt.Printf("kb:        %s\n", result.KBDir)
	fmt.Printf("specs:     %s\n", result.SpecsDir)
}

func handleSpec(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing spec subcommand"))
	}
	switch args[0] {
	case "lint":
		fs := flag.NewFlagSet("forge spec lint", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		specPath := fs.String("spec", "", "optional spec file or directory (defaults to all specs in the project)")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		report, err := svc.LintSpecs(*cwd, *specPath)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(report)
		} else {
			fmt.Print(formatSpecLintReport(report))
		}
		if !report.Valid {
			os.Exit(2)
		}
	case "generate":
		fs := flag.NewFlagSet("forge spec generate", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		background := fs.Bool("background", false, "spawn in a tmux window instead of running in foreground")
		_ = fs.Parse(args[1:])
		capability := strings.Join(fs.Args(), " ")
		if strings.TrimSpace(capability) == "" {
			fatal(fmt.Errorf("usage: forge spec generate [--cwd PATH] [--background] <capability description>"))
		}
		result, err := svc.GenerateSpec(context.Background(), *cwd, capability, *background)
		if err != nil {
			fatal(err)
		}
		fmt.Print(result.Output)
	default:
		fatal(fmt.Errorf("unknown spec subcommand: %s", args[0]))
	}
}

func handleTest(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing test subcommand"))
	}
	switch args[0] {
	case "setup":
		fs := flag.NewFlagSet("forge test setup", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		specPath := fs.String("spec", "", "required spec file to plan/apply validation setup for")
		bootstrap := fs.Bool("bootstrap", false, "recommend bootstrap setup when no suitable harness exists")
		apply := fs.Bool("apply", false, "write the generated validation plan back into the spec file")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(*specPath) == "" {
			fatal(fmt.Errorf("forge test setup requires --spec <file>"))
		}
		var (
			plan forge.TestSetupPlan
			err  error
		)
		if *apply {
			plan, err = svc.ApplyTestSetup(*cwd, *specPath, *bootstrap)
		} else {
			plan, err = svc.PlanTestSetup(*cwd, *specPath, *bootstrap)
		}
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(plan)
			return
		}
		fmt.Print(formatTestSetupPlan(plan))
	default:
		fatal(fmt.Errorf("unknown test subcommand: %s", args[0]))
	}
}

func handleRun(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing run subcommand"))
	}
	switch args[0] {
	case "plan":
		fs := flag.NewFlagSet("forge run plan", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		specPath := fs.String("spec", "", "optional spec file or directory (defaults to all specs in the project)")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		plan, err := svc.PlanRun(*cwd, *specPath)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(plan)
		} else {
			fmt.Print(formatRunPlan(plan))
		}
		if !plan.LintValid {
			os.Exit(2)
		}
	case "execute":
		fs := flag.NewFlagSet("forge run execute", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		specPath := fs.String("spec", "", "optional spec file or directory (defaults to all specs in the project)")
		apply := fs.Bool("apply", false, "execute run and persist state (default is preview/dry-run)")
		live := fs.Bool("live", false, "spawn agents in tmux panes (requires --apply)")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		var (
			result forge.RunExecuteResult
			err    error
		)
		if *live {
			if !*apply {
				fatal(fmt.Errorf("--live requires --apply"))
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			result, err = svc.ExecuteRunLive(ctx, *cwd, *specPath)
		} else if *apply {
			result, err = svc.ExecuteRun(*cwd, *specPath)
		} else {
			result, err = svc.PreviewRun(*cwd, *specPath)
		}
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(result)
		} else {
			fmt.Print(formatRunExecuteResult(result))
		}
		if !result.Plan.LintValid {
			os.Exit(2)
		}
	case "list":
		fs := flag.NewFlagSet("forge run list", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		records, err := svc.ListRunRecords(*cwd)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(records)
			return
		}
		if len(records) == 0 {
			fmt.Println("No run records found.")
			return
		}
		for _, r := range records {
			fmt.Printf("%s  %s  %s  total:%d ok:%d skip:%d fail:%d\n",
				r.RunID, r.Status, r.StartedAt,
				r.Summary.Total, r.Summary.Succeeded, r.Summary.Skipped, r.Summary.Failed)
		}
	case "show":
		fs := flag.NewFlagSet("forge run show", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		runID := fs.String("run-id", "", "run ID to inspect")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		if *runID == "" && len(fs.Args()) > 0 {
			*runID = fs.Args()[0]
		}
		if strings.TrimSpace(*runID) == "" {
			fatal(fmt.Errorf("forge run show requires --run-id or a positional run ID"))
		}
		record, err := svc.LoadRunRecord(*cwd, *runID)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(record)
			return
		}
		fmt.Print(formatRunRecord(record))
	case "watch":
		fs := flag.NewFlagSet("forge run watch", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		specPath := fs.String("spec", "", "optional spec to watch")
		interval := fs.Duration("interval", 10*time.Second, "poll interval")
		review := fs.Bool("review", true, "auto-spawn review agents after code completion")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		_ = asJSON // reserved for future structured watch output

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		cfg := forge.WatchConfig{
			CWD:          *cwd,
			SpecPath:     *specPath,
			PollInterval: *interval,
			AutoReview:   *review,
		}
		if err := svc.WatchRun(ctx, cfg); err != nil && err != context.Canceled {
			fatal(err)
		}
	default:
		fatal(fmt.Errorf("unknown run subcommand: %s", args[0]))
	}
}

func handleEvents(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing events subcommand (use: list | tail)"))
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("forge events list", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		runID := fs.String("run", "", "run ID to load events from (required)")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(*runID) == "" {
			fatal(fmt.Errorf("--run is required"))
		}
		events, err := svc.LoadRunEvents(*cwd, *runID)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(events)
			return
		}
		if len(events) == 0 {
			fmt.Println("No events for that run.")
			return
		}
		for _, e := range events {
			fmt.Printf("[%s] %s  %s — %s\n", e.Timestamp, e.TaskName, strings.ToUpper(e.Type), e.Message)
		}
	case "tail":
		fs := flag.NewFlagSet("forge events tail", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		runID := fs.String("run", "", "run ID to tail events from (required)")
		task := fs.String("task", "", "optional task name to filter")
		interval := fs.Duration("interval", time.Second, "poll interval")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(*runID) == "" {
			fatal(fmt.Errorf("--run is required"))
		}

		ctxResolved, err := svc.ResolveContext(*cwd)
		if err != nil {
			fatal(err)
		}

		base := ctxResolved.RepoLocalForge
		if base == "" {
			base = ctxResolved.ProjectRoot
		}
		runDir := filepath.Join(base, "runtime", *runID)

		// Print existing events first
		existing, _ := svc.LoadRunEvents(*cwd, *runID)
		for _, e := range existing {
			if *task != "" && !strings.EqualFold(e.TaskName, *task) {
				continue
			}
			fmt.Printf("[%s] %s  %s — %s\n", e.Timestamp, e.TaskName, strings.ToUpper(e.Type), e.Message)
		}

		// Then tail for new ones
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		// Find all events files in the run dir and tail them
		entries, err := os.ReadDir(runDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Waiting for events...")
				<-ctx.Done()
				return
			}
			fatal(err)
		}
		var wg sync.WaitGroup
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".events.jsonl") {
				continue
			}
			taskSlug := strings.TrimSuffix(name, ".events.jsonl")
			if *task != "" && !strings.EqualFold(taskSlug, *task) {
				continue
			}
			path := filepath.Join(runDir, name)
			wg.Add(1)
			go func(p, ts string) {
				defer wg.Done()
				// Seek past the initial content we already printed
				info, _ := os.Stat(p)
				initialSize := int64(0)
				if info != nil {
					initialSize = info.Size()
				}
				_ = forge.TailEventsFromOffset(ctx, p, initialSize, func(ev forge.Event) {
					fmt.Printf("[%s] %s  %s — %s\n", ev.Timestamp, ts, strings.ToUpper(ev.Type), ev.Message)
				}, *interval)
			}(path, taskSlug)
		}
		wg.Wait()
	default:
		fatal(fmt.Errorf("unknown events subcommand: %s", args[0]))
	}
}

func handleDecisions(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing decisions subcommand (use: list | show)"))
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("forge decisions list", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])

		grouped, err := svc.ListAllDecisions(*cwd)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(grouped)
			return
		}
		if len(grouped) == 0 {
			fmt.Println("No decisions recorded.")
			return
		}
		for specSlug, decisions := range grouped {
			fmt.Printf("Spec: %s\n", specSlug)
			for _, d := range decisions {
				fmt.Printf("  - [%s] %s — %s\n", d.TaskName, d.Choice, d.Rationale)
				if len(d.Alternatives) > 0 {
					fmt.Printf("      alternatives: %s\n", strings.Join(d.Alternatives, ", "))
				}
			}
			fmt.Println()
		}
	case "show":
		fs := flag.NewFlagSet("forge decisions show", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		specFile := fs.String("spec", "", "spec file name")
		taskName := fs.String("task", "", "task name (if omitted, shows all decisions for the spec)")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(*specFile) == "" {
			fatal(fmt.Errorf("--spec is required"))
		}

		var decisions []forge.Decision
		var err error
		if strings.TrimSpace(*taskName) != "" {
			decisions, err = svc.LoadDecisionsForTask(*cwd, *specFile, *taskName)
		} else {
			decisions, err = svc.LoadDecisionsForSpec(*cwd, *specFile)
		}
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(decisions)
			return
		}
		if len(decisions) == 0 {
			fmt.Println("No decisions recorded for that spec/task.")
			return
		}
		for _, d := range decisions {
			fmt.Printf("[%s] %s\n", d.TaskName, d.RecordedAt)
			fmt.Printf("  Choice: %s\n", d.Choice)
			if d.Rationale != "" {
				fmt.Printf("  Rationale: %s\n", d.Rationale)
			}
			if len(d.Alternatives) > 0 {
				fmt.Printf("  Alternatives: %s\n", strings.Join(d.Alternatives, ", "))
			}
			if d.RunID != "" {
				fmt.Printf("  Run: %s\n", d.RunID)
			}
			fmt.Println()
		}
	default:
		fatal(fmt.Errorf("unknown decisions subcommand: %s", args[0]))
	}
}

func handleMemory(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing memory subcommand (use: review)"))
	}
	switch args[0] {
	case "review":
		fs := flag.NewFlagSet("forge memory review", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])

		memories, err := svc.ListProposedMemories(*cwd)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(memories)
			return
		}
		if len(memories) == 0 {
			fmt.Println("No proposed memories pending review.")
			return
		}
		fmt.Printf("Proposed memories (%d):\n\n", len(memories))
		for i, m := range memories {
			fmt.Printf("  %d. %s\n", i+1, m.Text)
		}
		fmt.Println("\nTo promote an entry, edit `kb/memory.md` and add it under the appropriate section.")
		fmt.Println("Then remove the entry from `inbox.md` under '## Proposed memory'.")
	default:
		fatal(fmt.Errorf("unknown memory subcommand: %s", args[0]))
	}
}

func handleHistory(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing history subcommand (use: show | list)"))
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("forge history list", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])

		history, err := svc.ListTaskHistory(*cwd)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(history)
			return
		}
		if len(history) == 0 {
			fmt.Println("No task history found.")
			return
		}
		for specSlug, tasks := range history {
			fmt.Printf("Spec: %s\n", specSlug)
			for _, t := range tasks {
				fmt.Printf("  - %s\n", t)
			}
		}
	case "show":
		fs := flag.NewFlagSet("forge history show", flag.ExitOnError)
		cwd := fs.String("cwd", "", "path to resolve Forge context from")
		specFile := fs.String("spec", "", "spec file name (required)")
		taskName := fs.String("task", "", "task name (required)")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(*specFile) == "" || strings.TrimSpace(*taskName) == "" {
			fatal(fmt.Errorf("--spec and --task are required"))
		}

		attempts, err := svc.LoadAttempts(*cwd, *specFile, *taskName)
		if err != nil {
			fatal(err)
		}
		feedback, _ := svc.LoadFeedback(*cwd, *specFile, *taskName)

		if *asJSON {
			printJSON(map[string]any{
				"attempts":         attempts,
				"latestFeedback":   feedback,
			})
			return
		}
		fmt.Printf("History for %q in %s\n\n", *taskName, *specFile)
		if len(attempts) == 0 {
			fmt.Println("No attempts recorded.")
		}
		for _, a := range attempts {
			fmt.Printf("Attempt %d — %s (%s)\n", a.Attempt, strings.ToUpper(a.Status), a.StartedAt)
			if a.EndedAt != "" {
				fmt.Printf("  ended: %s\n", a.EndedAt)
			}
			if a.Error != "" {
				fmt.Printf("  error: %s\n", a.Error)
			}
			if a.PR != "" {
				fmt.Printf("  PR: %s\n", a.PR)
			}
			if len(a.ReviewFeedback) > 0 {
				fmt.Println("  review feedback:")
				for _, item := range a.ReviewFeedback {
					fmt.Printf("    - [%s] %s", item.Kind, item.Text)
					if item.File != "" {
						fmt.Printf(" (%s", item.File)
						if item.Line > 0 {
							fmt.Printf(":%d", item.Line)
						}
						fmt.Print(")")
					}
					fmt.Println()
				}
			}
		}
		if feedback != nil {
			fmt.Printf("\nLatest structured feedback (%s)\n", feedback.Status)
			for _, item := range feedback.Items {
				fmt.Printf("  - [%s] %s\n", item.Kind, item.Text)
			}
		}
	default:
		fatal(fmt.Errorf("unknown history subcommand: %s", args[0]))
	}
}

func handleMCP(svc *forge.Service, args []string) {
	if len(args) == 0 || args[0] != "serve" {
		fatal(fmt.Errorf("use: forge mcp serve"))
	}
	if err := svc.ServeMCP(); err != nil {
		fatal(err)
	}
}

func handleBoard(svc *forge.Service, args []string) {
	fs := flag.NewFlagSet("forge board", flag.ExitOnError)
	cwd := fs.String("cwd", "", "path to resolve Forge context from")
	asJSON := fs.Bool("json", false, "output JSON")
	web := fs.Bool("web", false, "start web UI server")
	port := fs.Int("port", 4242, "port for web UI server")
	watch := fs.Bool("watch", false, "also spawn the watch daemon to auto-execute ready tasks (requires --web)")
	interval := fs.Duration("interval", 10*time.Second, "watch poll interval")
	review := fs.Bool("review", true, "auto-spawn review agents when using --watch")
	_ = fs.Parse(args)

	if *web {
		if *watch {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			go func() {
				cfg := forge.WatchConfig{
					CWD:          *cwd,
					PollInterval: *interval,
					AutoReview:   *review,
				}
				if err := svc.WatchRun(ctx, cfg); err != nil && err != context.Canceled {
					fmt.Fprintf(os.Stderr, "watch daemon error: %v\n", err)
				}
			}()
		}
		if err := svc.ServeBoardWeb(*cwd, *port); err != nil {
			fatal(err)
		}
		return
	}

	board, err := svc.Board(*cwd)
	if err != nil {
		fatal(err)
	}
	if *asJSON {
		printJSON(board)
		return
	}
	fmt.Print(forge.FormatBoard(board))
}

func handleDev(_ *forge.Service, args []string) {
	if len(args) == 0 {
		// default: detect + generate for cwd
		handleDevDetect(args)
		return
	}
	switch args[0] {
	case "start":
		fs := flag.NewFlagSet("forge dev start", flag.ExitOnError)
		_ = fs.Parse(args[1:])
		remaining := fs.Args()
		if len(remaining) == 0 {
			fatal(fmt.Errorf("usage: forge dev start <project>"))
		}
		handleDevStart(remaining[0])
	case "ls", "list":
		handleDevList(args[1:])
	case "kill":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: forge dev kill <project|--all>"))
		}
		handleDevKill(args[1:])
	case "attach", "a":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: forge dev attach <project>"))
		}
		handleDevAttach(args[1])
	case "edit":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: forge dev edit <project>"))
		}
		handleDevEdit(args[1])
	case "detect":
		handleDevDetect(args[1:])
	case "all":
		handleDevAll()
	case "help", "-h", "--help":
		fmt.Print(`forge dev — tmux session manager for multi-project development

Usage:
  forge dev                  Detect services in cwd and generate config
  forge dev <project>        Start a project session
  forge dev ls [--json]      List projects and status
  forge dev kill <project>   Kill a session
  forge dev kill --all       Kill all dev sessions
  forge dev attach <project> Attach to a running session
  forge dev edit <project>   Edit a project config
  forge dev detect [PATH]    Detect services and write config
  forge dev all              Start all configured projects
`)
	default:
		// treat as project name: forge dev amplify → start
		handleDevStart(args[0])
	}
}

func handleDevStart(project string) {
	path, err := forge.DevConfigPath(project)
	if err != nil {
		fatal(err)
	}
	cfg, err := forge.ParseDevConfig(path)
	if err != nil {
		fatal(fmt.Errorf("no config for %q: %w\nGenerate one: forge dev detect", project, err))
	}

	fmt.Printf("Starting %s (%d services)\n", project, len(cfg.Services))
	if err := forge.DevStartSession(cfg); err != nil {
		fatal(err)
	}
	for _, svc := range cfg.Services {
		fmt.Printf("  %s\n", svc.Name)
	}
	fmt.Printf("\n%s running. Attach: forge dev attach %s\n", project, project)
}

func handleDevList(args []string) {
	fs := flag.NewFlagSet("forge dev ls", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	sessions, err := forge.DevListSessions()
	if err != nil {
		fatal(err)
	}

	if *asJSON {
		printJSON(sessions)
		return
	}

	if len(sessions) == 0 {
		fmt.Println("No dev configs found.")
		fmt.Println("Generate one: forge dev detect")
		return
	}

	for _, s := range sessions {
		status := "stopped"
		if s.Running {
			status = "running"
		}
		fmt.Printf("  %-20s %-10s %s  [%d services]\n", s.Project, status, s.Root, s.ServiceCount)
	}
}

func handleDevKill(args []string) {
	if args[0] == "--all" {
		sessions, err := forge.DevListSessions()
		if err != nil {
			fatal(err)
		}
		killed := 0
		for _, s := range sessions {
			if s.Running {
				if err := forge.DevKillSession(s.Session); err == nil {
					fmt.Printf("Killed %s\n", s.Project)
					killed++
				}
			}
		}
		if killed == 0 {
			fmt.Println("No active dev sessions.")
		}
		return
	}

	project := args[0]
	path, err := forge.DevConfigPath(project)
	if err != nil {
		fatal(err)
	}
	cfg, err := forge.ParseDevConfig(path)
	if err != nil {
		// try using project as session name directly
		if err := forge.DevKillSession(project); err != nil {
			fatal(err)
		}
		fmt.Printf("Killed %s\n", project)
		return
	}
	if err := forge.DevKillSession(cfg.Session); err != nil {
		fatal(err)
	}
	fmt.Printf("Killed %s\n", project)
}

func handleDevAttach(project string) {
	path, err := forge.DevConfigPath(project)
	if err != nil {
		fatal(err)
	}
	session := project
	if cfg, err := forge.ParseDevConfig(path); err == nil {
		session = cfg.Session
	}
	if !forge.DevSessionRunning(session) {
		fatal(fmt.Errorf("no active session %q — start it: forge dev %s", session, project))
	}
	if err := forge.AttachToSession(session); err != nil {
		fatal(err)
	}
}

func handleDevEdit(project string) {
	path, err := forge.DevConfigPath(project)
	if err != nil {
		fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		fatal(fmt.Errorf("no config for %q: %w", project, err))
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}

func handleDevDetect(args []string) {
	fs := flag.NewFlagSet("forge dev detect", flag.ExitOnError)
	name := fs.String("name", "", "project name (default: derived from directory)")
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	remaining := fs.Args()

	// Multi-dir mode: multiple paths given, or no paths (interactive)
	if len(remaining) != 1 {
		handleDevDetectMulti(*name, *asJSON, remaining)
		return
	}

	// Single-dir mode: one path given
	root := remaining[0]
	detected, err := forge.DetectDevServices(root)
	if err != nil {
		fatal(err)
	}

	if *asJSON {
		printJSON(detected)
		return
	}

	project := detected.ProjectName
	if *name != "" {
		project = *name
	}

	if len(detected.Services) == 0 {
		fmt.Printf("No services detected in %s\n", detected.Root)
		return
	}

	printDetectedServices(detected)

	cfg := forge.DevConfig{
		Root:     detected.Root,
		Session:  project,
		Services: detected.Services,
	}

	path, err := forge.WriteDevConfig(project, cfg)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("\nConfig written: %s\n", path)
	fmt.Printf("Start:  forge dev %s\n", project)
	fmt.Printf("Edit:   forge dev edit %s\n", project)
}

func handleDevDetectMulti(name string, asJSON bool, paths []string) {
	// If explicit paths given, skip interactive and merge directly
	if len(paths) > 1 {
		handleDevDetectMerge(name, asJSON, paths)
		return
	}

	// Interactive mode: scan cwd, let user pick folders
	dir := "."
	if len(paths) == 1 {
		dir = paths[0]
	}

	result, err := forge.DevDetectInteractive(dir)
	if err != nil {
		fatal(err)
	}

	if asJSON {
		printJSON(result)
		return
	}

	fmt.Printf("\nDetected %d services across %d folders:\n", len(result.Config.Services), len(result.Detected))
	for _, svc := range result.Config.Services {
		dir := ""
		if svc.Dir != "" {
			dir = fmt.Sprintf(" (dir: %s)", svc.Dir)
		}
		wait := ""
		if svc.Wait > 0 {
			wait = fmt.Sprintf(" [wait: %ds]", svc.Wait)
		}
		fmt.Printf("  %-20s %s%s%s\n", svc.Name, svc.Cmd, dir, wait)
	}

	fmt.Printf("\nConfig written: %s\n", result.ConfigPath)
	fmt.Printf("Start:  forge dev %s\n", result.ProjectName)
	fmt.Printf("Edit:   forge dev edit %s\n", result.ProjectName)
}

func handleDevDetectMerge(name string, asJSON bool, paths []string) {
	// Find common parent
	absPaths := make([]string, len(paths))
	for i, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			fatal(err)
		}
		absPaths[i] = abs
	}
	commonParent := findCommonParent(absPaths)

	var allServices []forge.DevService
	var allDetected []forge.DetectedProject

	for _, p := range absPaths {
		detected, err := forge.DetectDevServices(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", p, err)
			continue
		}
		allDetected = append(allDetected, detected)

		folderName := filepath.Base(p)
		for i := range detected.Services {
			svc := &detected.Services[i]
			if len(paths) > 1 {
				svc.Name = folderName + "-" + svc.Name
			}
			if svc.Dir != "" {
				svc.Dir = filepath.Join(folderName, svc.Dir)
			} else {
				svc.Dir = folderName
			}
		}
		allServices = append(allServices, detected.Services...)
	}

	if len(allServices) == 0 {
		fmt.Println("No services detected in given paths.")
		return
	}

	project := name
	if project == "" {
		project = filepath.Base(commonParent)
	}

	if asJSON {
		printJSON(allDetected)
		return
	}

	cfg := forge.DevConfig{
		Root:     commonParent,
		Session:  project,
		Services: allServices,
	}

	path, err := forge.WriteDevConfig(project, cfg)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Detected %d services across %d folders:\n", len(allServices), len(paths))
	for _, svc := range allServices {
		dir := ""
		if svc.Dir != "" {
			dir = fmt.Sprintf(" (dir: %s)", svc.Dir)
		}
		fmt.Printf("  %-20s %s%s\n", svc.Name, svc.Cmd, dir)
	}

	fmt.Printf("\nConfig written: %s\n", path)
	fmt.Printf("Start:  forge dev %s\n", project)
	fmt.Printf("Edit:   forge dev edit %s\n", project)
}

func printDetectedServices(detected forge.DetectedProject) {
	fmt.Printf("Detected %d services in %s:\n", len(detected.Services), detected.Root)
	for _, svc := range detected.Services {
		dir := ""
		if svc.Dir != "" {
			dir = fmt.Sprintf(" (dir: %s)", svc.Dir)
		}
		wait := ""
		if svc.Wait > 0 {
			wait = fmt.Sprintf(" [wait: %ds]", svc.Wait)
		}
		fmt.Printf("  %-15s %s%s%s\n", svc.Name, svc.Cmd, dir, wait)
	}
}

func findCommonParent(paths []string) string {
	if len(paths) == 0 {
		return "."
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}
	common := paths[0]
	for _, p := range paths[1:] {
		for !strings.HasPrefix(p, common+"/") && common != "/" {
			common = filepath.Dir(common)
		}
	}
	return common
}

func handleDevAll() {
	sessions, err := forge.DevListSessions()
	if err != nil {
		fatal(err)
	}
	if len(sessions) == 0 {
		fmt.Println("No dev configs found.")
		return
	}
	for _, s := range sessions {
		if s.Running {
			fmt.Printf("  %s — already running\n", s.Project)
			continue
		}
		path, err := forge.DevConfigPath(s.Project)
		if err != nil {
			continue
		}
		cfg, err := forge.ParseDevConfig(path)
		if err != nil {
			continue
		}
		if err := forge.DevStartSession(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "  %s — error: %v\n", s.Project, err)
			continue
		}
		fmt.Printf("  %s — started (%d services)\n", s.Project, s.ServiceCount)
	}
}

func handleProjectCreateExisting(svc *forge.Service, cwd, workspace, projectID, name, pathFlag, reposFlag string, apply, asJSON, setupDev bool) {
	dir := cwd
	if dir == "" {
		if pathFlag != "" {
			dir = pathFlag
		} else {
			d, err := os.Getwd()
			if err != nil {
				fatal(err)
			}
			dir = d
		}
	}

	var repoRefs []forge.ExistingRepoRef

	if reposFlag != "" {
		// non-interactive: use provided paths
		for _, p := range strings.Split(reposFlag, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			repoRefs = append(repoRefs, forge.ExistingRepoRef{Path: p})
		}
	} else if apply {
		// interactive: show huh multi-select
		absDir, err := filepath.Abs(dir)
		if err != nil {
			fatal(err)
		}
		candidates := forge.ScanCandidateDirs(absDir)
		if len(candidates) == 0 {
			fatal(fmt.Errorf("no project directories found in %s", absDir))
		}

		options := make([]huh.Option[string], len(candidates))
		for i, c := range candidates {
			label := c.Name
			if c.Hint != "" {
				label = fmt.Sprintf("%s  (%s)", c.Name, c.Hint)
			}
			options[i] = huh.NewOption(label, c.Path)
		}

		var selectedPaths []string
		var projectName string
		defaultName := filepath.Base(absDir)
		if name != "" {
			defaultName = name
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("Select repos to include").
					Description(absDir).
					Options(options...).
					Value(&selectedPaths),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Project name").
					Description("Used for Forge project ID and tmux session name").
					Placeholder(defaultName).
					Value(&projectName),
			),
		)

		if err := form.Run(); err != nil {
			fatal(err)
		}

		if len(selectedPaths) == 0 {
			fatal(fmt.Errorf("no repos selected"))
		}

		for _, p := range selectedPaths {
			repoRefs = append(repoRefs, forge.ExistingRepoRef{Path: p})
		}

		if strings.TrimSpace(projectName) != "" {
			name = projectName
		} else if name == "" {
			name = defaultName
		}
	} else {
		// plan mode without --repos: show what we'd detect
		absDir, err := filepath.Abs(dir)
		if err != nil {
			fatal(err)
		}
		candidates := forge.ScanCandidateDirs(absDir)
		if len(candidates) == 0 {
			fmt.Printf("No project directories found in %s\n", absDir)
			return
		}
		fmt.Printf("Candidate repos in %s:\n", absDir)
		for _, c := range candidates {
			hint := ""
			if c.Hint != "" {
				hint = fmt.Sprintf("  (%s)", c.Hint)
			}
			fmt.Printf("  %s%s\n", c.Name, hint)
		}
		fmt.Println("\nTo create the project interactively:")
		fmt.Printf("  forge project create --existing --apply\n")
		fmt.Println("\nTo create non-interactively:")
		fmt.Printf("  forge project create --existing --name <name> --repos <path1,path2> --apply\n")
		return
	}

	if len(repoRefs) == 0 {
		fatal(fmt.Errorf("no repos specified"))
	}

	input := forge.ProjectCreateExistingInput{
		CWD:         dir,
		Workspace:   workspace,
		ProjectID:   projectID,
		ProjectName: name,
		Repos:       repoRefs,
		SetupDev:    setupDev,
	}

	var (
		plan forge.ProjectCreateExistingPlan
		err  error
	)
	if apply {
		plan, err = svc.ApplyProjectCreateExisting(input)
	} else {
		plan, err = svc.PlanProjectCreateExisting(input)
	}
	if err != nil {
		fatal(err)
	}

	if asJSON {
		printJSON(plan)
		return
	}

	fmt.Print(formatProjectCreateExistingPlan(plan))
}

func formatProjectCreateExistingPlan(plan forge.ProjectCreateExistingPlan) string {
	var b strings.Builder
	action := "Plan"
	if plan.Updated {
		action = "Created"
	}
	fmt.Fprintf(&b, "%s project: %s (%s)\n", action, plan.ProjectName, plan.ProjectID)
	fmt.Fprintf(&b, "  workspace:  %s\n", plan.Workspace)
	fmt.Fprintf(&b, "  root:       %s\n", plan.ProjectRoot)
	fmt.Fprintf(&b, "  topology:   %s\n", plan.Topology.Mode)
	if len(plan.Repos) > 0 {
		b.WriteString("\nRepos:\n")
		for _, r := range plan.Repos {
			kind := ""
			for _, c := range plan.Components {
				if c.ComponentID == r.RepoID {
					kind = c.Kind
					break
				}
			}
			if kind != "" {
				fmt.Fprintf(&b, "  %s [%s]  %s\n", r.RepoID, kind, r.Path)
			} else {
				fmt.Fprintf(&b, "  %s  %s\n", r.RepoID, r.Path)
			}
		}
	}
	if len(plan.MetadataChanges) > 0 {
		b.WriteString("\nChanges:\n")
		for _, c := range plan.MetadataChanges {
			fmt.Fprintf(&b, "  - %s\n", c)
		}
	}
	if plan.DevConfigPath != "" {
		fmt.Fprintf(&b, "\nDev config: %s\n", plan.DevConfigPath)
		fmt.Fprintf(&b, "  Start:  forge dev %s\n", plan.ProjectID)
	}
	if len(plan.Warnings) > 0 {
		b.WriteString("\nWarnings:\n")
		for _, w := range plan.Warnings {
			fmt.Fprintf(&b, "  ! %s\n", w)
		}
	}
	if len(plan.NextSteps) > 0 && plan.Updated {
		b.WriteString("\nNext steps:\n")
		for _, s := range plan.NextSteps {
			fmt.Fprintf(&b, "  - %s\n", s)
		}
	}
	return b.String()
}

func handlePI(svc *forge.Service, args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("missing pi subcommand"))
	}
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("forge pi install", flag.ExitOnError)
		pkg := fs.String("package-path", findPackageRootOrCwd(), "path to the forge pi package")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		result, err := svc.PIInstall(*pkg)
		if err != nil {
			fatal(err)
		}
		printResult(*asJSON, result, fmt.Sprintf("Pi package installed from %s", result.PackagePath))
	case "uninstall":
		fs := flag.NewFlagSet("forge pi uninstall", flag.ExitOnError)
		pkg := fs.String("package-path", findPackageRootOrCwd(), "path to the forge pi package")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		result, err := svc.PIUninstall(*pkg)
		if err != nil {
			fatal(err)
		}
		printResult(*asJSON, result, fmt.Sprintf("Pi package removed: %s", result.PackagePath))
	case "status":
		fs := flag.NewFlagSet("forge pi status", flag.ExitOnError)
		pkg := fs.String("package-path", findPackageRootOrCwd(), "path to the forge pi package")
		asJSON := fs.Bool("json", false, "output JSON")
		_ = fs.Parse(args[1:])
		result, err := svc.PIStatus(*pkg)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(result)
			return
		}
		status := "not installed"
		if result.Installed {
			status = "installed"
		}
		fmt.Printf("%s\nsettings: %s\npackage:  %s\n", status, result.SettingsPath, result.PackagePath)
	default:
		fatal(fmt.Errorf("unknown pi subcommand: %s", args[0]))
	}
}

// ---------------------------------------------------------------------------
// Daemon command
// ---------------------------------------------------------------------------

func handleDaemon(args []string) {
	if len(args) == 0 {
		handleDaemonStatus()
		return
	}
	switch args[0] {
	case "start":
		handleDaemonStart(args[1:])
	case "status":
		handleDaemonStatus()
	case "stop":
		handleDaemonStop()
	case "help", "-h", "--help":
		fmt.Print(`forge daemon — Background sync with Forge Cloud

Usage:
  forge daemon start [--project ID]   Start the daemon (foreground)
  forge daemon status                 Show daemon status
  forge daemon stop                   Stop the daemon
`)
	default:
		fatal(fmt.Errorf("unknown daemon subcommand: %s", args[0]))
	}
}

func handleDaemonStart(args []string) {
	fs := flag.NewFlagSet("forge daemon start", flag.ExitOnError)
	projectID := fs.String("project", "", "project ID to sync")
	_ = fs.Parse(args)

	if err := cloud.RunDaemon(*projectID); err != nil {
		fatal(err)
	}
}

func handleDaemonStatus() {
	status := cloud.GetDaemonStatus()

	if !status.Running {
		fmt.Println("Daemon: not running")
		fmt.Println("Start: forge daemon start --project <id>")
		return
	}

	fmt.Printf("Daemon: running (PID %d)\n", status.PID)
	if status.Connected {
		fmt.Println("Cloud: connected")
	} else {
		fmt.Println("Cloud: disconnected")
	}
	if status.ProjectID != "" {
		fmt.Printf("Project: %s\n", status.ProjectID)
	}
	fmt.Printf("Tasks: %d\n", status.TaskCount)
	if !status.LastPing.IsZero() {
		fmt.Printf("Last ping: %s\n", status.LastPing.Format(time.RFC3339))
	}
}

func handleDaemonStop() {
	status := cloud.GetDaemonStatus()
	if !status.Running {
		fmt.Println("Daemon is not running.")
		return
	}

	process, err := os.FindProcess(status.PID)
	if err != nil {
		fatal(fmt.Errorf("finding process: %w", err))
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		fatal(fmt.Errorf("stopping daemon: %w", err))
	}

	fmt.Println("Daemon stopped.")
}

// ---------------------------------------------------------------------------
// Task commands (cloud-integrated)
// ---------------------------------------------------------------------------

func handleTask(svc *forge.Service, args []string) {
	if len(args) == 0 {
		handleTaskList(args)
		return
	}
	switch args[0] {
	case "start":
		handleTaskStart(svc, args[1:])
	case "list", "ls":
		handleTaskList(args[1:])
	case "status":
		handleTaskStatus(args[1:])
	case "done":
		handleTaskDone(args[1:])
	case "help", "-h", "--help":
		fmt.Print(`forge task — Cloud task management

Usage:
  forge task list [--json]        List assigned tasks from cloud
  forge task start <task-id>      Start working on a cloud task (spawns Claude in tmux)
  forge task status <task-id>     Show status of a task
  forge task done <task-id>       Mark task as complete
`)
	default:
		// Assume it's a task ID - start it
		handleTaskStart(svc, args)
	}
}

func handleTaskStart(svc *forge.Service, args []string) {
	fs := flag.NewFlagSet("forge task start", flag.ExitOnError)
	cwd := fs.String("cwd", "", "path to resolve Forge context from")
	_ = fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) == 0 {
		fatal(fmt.Errorf("usage: forge task start <task-id>"))
	}
	taskID := remaining[0]

	// Get cloud client
	client, err := cloud.NewClientFromStored()
	if err != nil {
		fatal(err)
	}

	// Resolve project ID from context
	ctx, err := svc.ResolveContext(*cwd)
	if err != nil {
		fatal(err)
	}

	runner := forge.NewCloudTaskRunner(svc, client, ctx.ProjectID)

	fmt.Printf("Starting task %s...\n", taskID)

	session, err := runner.StartCloudTask(context.Background(), taskID, *cwd)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("\nTask session started:\n")
	fmt.Printf("  Task:     %s\n", session.TaskName)
	fmt.Printf("  Spec:     %s\n", session.SpecTitle)
	fmt.Printf("  Branch:   %s\n", session.Branch)
	fmt.Printf("  Worktree: %s\n", session.WorktreePath)
	fmt.Printf("  Tmux:     %s (window: %s)\n", session.Session, session.Window)
	fmt.Printf("\nAttach: tmux attach -t %s\n", session.Session)
}

func handleTaskList(args []string) {
	fs := flag.NewFlagSet("forge task list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	client, err := cloud.NewClientFromStored()
	if err != nil {
		fatal(err)
	}

	tasks, err := client.ListAssignedTasks()
	if err != nil {
		fatal(err)
	}

	if *asJSON {
		printJSON(tasks)
		return
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks assigned to you.")
		fmt.Println("\nTip: Tasks are assigned in Forge Cloud (your team's project board).")
		return
	}

	fmt.Printf("Your tasks (%d):\n\n", len(tasks))
	for _, t := range tasks {
		statusIcon := "○"
		switch t.Status {
		case "in_progress":
			statusIcon = "●"
		case "in_review":
			statusIcon = "◐"
		case "done":
			statusIcon = "✓"
		case "blocked":
			statusIcon = "✗"
		}
		fmt.Printf("  %s [%s] %s\n", statusIcon, t.ID[:8], t.Name)
		if t.Repo != "" {
			fmt.Printf("       repo: %s\n", t.Repo)
		}
	}
	fmt.Printf("\nStart a task: forge task start <task-id>\n")
}

func handleTaskStatus(args []string) {
	fs := flag.NewFlagSet("forge task status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) == 0 {
		fatal(fmt.Errorf("usage: forge task status <task-id>"))
	}
	taskID := remaining[0]

	client, err := cloud.NewClientFromStored()
	if err != nil {
		fatal(err)
	}

	task, err := client.GetTask(taskID)
	if err != nil {
		fatal(err)
	}

	if *asJSON {
		printJSON(task)
		return
	}

	fmt.Printf("Task: %s\n", task.Name)
	fmt.Printf("ID: %s\n", task.ID)
	fmt.Printf("Status: %s\n", task.Status)
	if task.Branch != "" {
		fmt.Printf("Branch: %s\n", task.Branch)
	}
	if task.PR != "" {
		fmt.Printf("PR: %s\n", task.PR)
	}
	if task.AssignedTo != "" {
		fmt.Printf("Assigned to: %s\n", task.AssignedTo)
	}
}

func handleTaskDone(args []string) {
	fs := flag.NewFlagSet("forge task done", flag.ExitOnError)
	status := fs.String("status", "in_review", "status to set (in_review, done)")
	_ = fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) == 0 {
		fatal(fmt.Errorf("usage: forge task done <task-id>"))
	}
	taskID := remaining[0]

	client, err := cloud.NewClientFromStored()
	if err != nil {
		fatal(err)
	}

	err = client.UpdateTaskStatus(taskID, cloud.TaskStatusUpdate{
		TaskID:    taskID,
		Status:    *status,
		Timestamp: time.Now(),
	})
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Task %s marked as %s\n", taskID, *status)
}

// ---------------------------------------------------------------------------
// Cloud commands
// ---------------------------------------------------------------------------

func handleCloud(_ *forge.Service, args []string) {
	if len(args) == 0 {
		handleCloudStatus()
		return
	}
	switch args[0] {
	case "login":
		handleCloudLogin(args[1:])
	case "logout":
		handleCloudLogout()
	case "status":
		handleCloudStatus()
	case "tasks":
		handleCloudTasks(args[1:])
	case "sync":
		handleCloudSync(args[1:])
	case "notify":
		handleCloudNotify(args[1:])
	case "help", "-h", "--help":
		fmt.Print(`forge cloud — Forge Cloud integration

Usage:
  forge cloud login             Authenticate with Forge Cloud
  forge cloud logout            Clear stored credentials
  forge cloud status            Show connection status
  forge cloud tasks [--json]    List assigned tasks
  forge cloud sync              Sync local state with cloud
  forge cloud notify [--test]   Test/configure notifications
`)
	default:
		fatal(fmt.Errorf("unknown cloud subcommand: %s", args[0]))
	}
}

func handleCloudNotify(args []string) {
	fs := flag.NewFlagSet("forge cloud notify", flag.ExitOnError)
	test := fs.Bool("test", false, "send a test notification")
	_ = fs.Parse(args)

	if *test {
		fmt.Println("Sending test notification...")
		err := cloud.Notify(cloud.Notification{
			Title:   "Forge Test",
			Message: "Notifications are working!",
			Level:   cloud.NotifySuccess,
			Sound:   true,
		})
		if err != nil {
			fatal(err)
		}
		fmt.Println("Test notification sent.")
		return
	}

	// Show current notification preferences
	prefs := cloud.GetNotificationPrefs()
	fmt.Println("Notification settings:")
	fmt.Printf("  Enabled:          %v\n", prefs.Enabled)
	fmt.Printf("  Desktop:          %v\n", prefs.Desktop)
	fmt.Printf("  Sound:            %v\n", prefs.Sound)
	fmt.Printf("  Task assigned:    %v\n", prefs.TaskAssigned)
	fmt.Printf("  Task completed:   %v\n", prefs.TaskCompleted)
	fmt.Printf("  Review feedback:  %v\n", prefs.ReviewFeedback)
	fmt.Printf("  Connection:       %v\n", prefs.ConnectionEvents)
	fmt.Println("\nTest: forge cloud notify --test")
}

func handleCloudLogin(args []string) {
	fs := flag.NewFlagSet("forge cloud login", flag.ExitOnError)
	cloudURL := fs.String("url", cloud.DefaultCloudURL, "Forge Cloud API URL")
	_ = fs.Parse(args)

	cfg := cloud.DefaultConfig()
	cfg.CloudURL = *cloudURL

	client := cloud.NewClient(cfg)

	fmt.Println("Initiating login...")
	deviceCode, err := client.LoginWithDeviceCode()
	if err != nil {
		fatal(err)
	}

	fmt.Printf("\nVisit: %s\n", deviceCode.VerificationURL)
	fmt.Printf("Enter code: %s\n\n", deviceCode.UserCode)
	fmt.Println("Waiting for authentication...")

	creds, err := client.PollForToken(deviceCode.DeviceCode, deviceCode.Interval)
	if err != nil {
		fatal(err)
	}

	if err := cloud.SaveCredentials(*creds); err != nil {
		fatal(fmt.Errorf("saving credentials: %w", err))
	}

	fmt.Printf("\nLogged in as %s\n", creds.Email)
}

func handleCloudLogout() {
	if err := cloud.ClearCredentials(); err != nil {
		fatal(err)
	}
	fmt.Println("Logged out.")
}

func handleCloudStatus() {
	creds, err := cloud.LoadCredentials()
	if err != nil {
		fmt.Println("Not logged in.")
		fmt.Println("Run: forge cloud login")
		return
	}

	fmt.Printf("Logged in as: %s\n", creds.Email)
	fmt.Printf("User ID: %s\n", creds.UserID)
	if creds.TeamID != "" {
		fmt.Printf("Team: %s\n", creds.TeamID)
	}
	fmt.Printf("Cloud URL: %s\n", creds.CloudURL)

	if creds.IsExpired() {
		fmt.Println("Token: expired (will refresh on next request)")
	} else if creds.NeedsRefresh() {
		fmt.Println("Token: expiring soon")
	} else {
		fmt.Println("Token: valid")
	}
}

func handleCloudTasks(args []string) {
	fs := flag.NewFlagSet("forge cloud tasks", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	client, err := cloud.NewClientFromStored()
	if err != nil {
		fatal(err)
	}

	tasks, err := client.ListAssignedTasks()
	if err != nil {
		fatal(err)
	}

	if *asJSON {
		printJSON(tasks)
		return
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks assigned to you.")
		return
	}

	fmt.Printf("Assigned tasks (%d):\n\n", len(tasks))
	for _, t := range tasks {
		fmt.Printf("  [%s] %s\n", t.Status, t.Name)
		if t.Repo != "" {
			fmt.Printf("         repo: %s\n", t.Repo)
		}
		if t.Branch != "" {
			fmt.Printf("         branch: %s\n", t.Branch)
		}
	}
}

func handleCloudSync(args []string) {
	fs := flag.NewFlagSet("forge cloud sync", flag.ExitOnError)
	projectID := fs.String("project", "", "project ID to sync")
	_ = fs.Parse(args)

	client, err := cloud.NewClientFromStored()
	if err != nil {
		fatal(err)
	}

	fmt.Println("Syncing with Forge Cloud...")

	// Create syncer
	syncer := cloud.NewSyncer(client, *projectID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := syncer.Start(ctx); err != nil {
		fatal(err)
	}
	defer syncer.Stop()

	// Get synced state
	state := syncer.State()
	tasks := state.GetAssignedTasks()

	fmt.Printf("Synced %d assigned tasks.\n", len(tasks))

	if *projectID != "" {
		kb := state.GetKnowledge(*projectID)
		if kb != nil {
			fmt.Println("Knowledge base synced.")
		}
	}

	fmt.Println("Done.")
}

func printResult(asJSON bool, value any, human string) {
	if asJSON {
		printJSON(value)
		return
	}
	fmt.Println(human)
}

func printJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(data))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func usage() {
	fmt.Print(`forge - Forge workspace CLI

Commands:
  forge workspace init   --name main [--path PATH]
  forge workspace list
  forge project init     --workspace main --project-id acme-platform --name "Acme Platform"
  forge project list     --workspace main
  forge project create   --name "Acme Platform" [--pack blank|nextjs-saas] [--packs-dir PATHS] [--topology ...] [--apply]
  forge project create   --existing --name "Floki" [--repos path1,path2] [--no-dev-setup] [--apply]
  forge project add      api [--as repo|app|package|module] [--apply]
  forge repo link        --workspace main --project-id acme-platform --repo-id web [--path .]
  forge repo list        --workspace main --project-id acme-platform
  forge spec lint        [--cwd PATH] [--spec FILE|DIR]
  forge spec generate    [--cwd PATH] [--background] <capability description>
  forge test setup       --spec FILE [--cwd PATH] [--bootstrap] [--apply]
  forge run plan         [--cwd PATH] [--spec FILE|DIR]
  forge run execute      [--cwd PATH] [--spec FILE|DIR] [--apply] [--live]
  forge run watch        [--cwd PATH] [--spec FILE|DIR] [--interval 10s] [--review]
  forge run list         [--cwd PATH]
  forge run show         [--cwd PATH] --run-id RUN_ID
  forge board            [--cwd PATH] [--json] [--web] [--port 4242] [--watch] [--interval 10s] [--review]
  forge history list     [--cwd PATH] [--json]
  forge history show     --spec FILE --task NAME [--cwd PATH] [--json]
  forge memory review    [--cwd PATH] [--json]
  forge events list      --run RUN_ID [--cwd PATH] [--json]
  forge events tail      --run RUN_ID [--task NAME] [--interval 1s] [--cwd PATH]
  forge decisions list   [--cwd PATH] [--json]
  forge decisions show   --spec FILE [--task NAME] [--cwd PATH] [--json]
  forge dev              [--name NAME] [PATH]              (detect services and generate config)
  forge dev <project>    Start a project's dev session
  forge dev ls           [--json]                          (list projects and status)
  forge dev kill         <project|--all>
  forge dev attach       <project>
  forge dev edit         <project>
  forge dev detect       [--name NAME] [PATH]
  forge dev all          Start all configured projects
  forge cloud login      Authenticate with Forge Cloud
  forge cloud logout     Clear stored credentials
  forge cloud status     Show connection status
  forge cloud tasks      [--json]                          (list assigned tasks)
  forge cloud sync       [--project ID]                    (sync local state with cloud)
  forge task list        [--json]                          (list your assigned tasks)
  forge task start       <task-id>                         (start a cloud task in tmux)
  forge task status      <task-id>                         (show task status)
  forge task done        <task-id> [--status in_review]    (mark task complete)
  forge daemon start     [--project ID]                    (start background cloud sync)
  forge daemon status                                      (show daemon status)
  forge daemon stop                                        (stop the daemon)
  forge mcp serve        (reads JSON-RPC from stdin, writes to stdout; for MCP-compatible hosts)
  forge context resolve  [--cwd PATH]
  forge pi install       [--package-path PATH]
  forge pi uninstall     [--package-path PATH]
  forge pi status        [--package-path PATH]

Environment:
  FORGE_HOME          Override ~/.forge
  PI_CODING_AGENT_DIR Override ~/.pi/agent
`)
}

func formatSpecLintReport(report forge.SpecLintReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Forge Spec Lint — %s (%s)\n", report.ProjectName, report.ProjectID)
	fmt.Fprintf(&b, "Specs checked: %d | errors: %d | warnings: %d\n", len(report.Specs), report.ErrorCount, report.WarningCount)
	if len(report.Specs) > 0 {
		fmt.Fprintf(&b, "Specs dir: %s\n", report.SpecsDir)
	}
	if len(report.Issues) == 0 {
		b.WriteString("\nNo lint issues found.\n")
		return b.String()
	}
	b.WriteString("\n")
	for _, issue := range report.Issues {
		level := strings.ToUpper(issue.Severity)
		location := issue.File
		if issue.Line > 0 {
			location = fmt.Sprintf("%s:%d", issue.File, issue.Line)
		}
		fmt.Fprintf(&b, "%s  %s", level, location)
		if issue.Task != "" {
			fmt.Fprintf(&b, " [%s]", issue.Task)
		}
		fmt.Fprintf(&b, "  %s\n", issue.Message)
		if len(issue.RelatedTasks) > 0 {
			fmt.Fprintf(&b, "      related: %s\n", strings.Join(issue.RelatedTasks, ", "))
		}
	}
	if reportHasCanonicalSpecGuidance(report) {
		b.WriteString("\nTip: canonical Forge specs should include:\n")
		b.WriteString("- ## Expected behavior\n")
		b.WriteString("- ## Test cases with ### Scenario headings and Given/When/Then steps\n")
		b.WriteString("- ## Validation plan\n")
		b.WriteString("- **Validation:** blocks in tasks with '- Covers: Scenario ...' lines\n")
	}
	return b.String()
}

func reportHasCanonicalSpecGuidance(report forge.SpecLintReport) bool {
	for _, issue := range report.Issues {
		switch issue.Code {
		case "missing_expected_behavior", "missing_test_cases", "missing_validation_plan", "missing_task_validation", "missing_task_coverage", "unknown_covered_scenario", "uncovered_scenario", "scenario_missing_given", "scenario_missing_when", "scenario_missing_then":
			return true
		}
	}
	return false
}

func formatRunPlan(plan forge.RunPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Forge Run Plan — %s (%s)\n", plan.ProjectName, plan.ProjectID)
	fmt.Fprintf(&b, "Lint valid: %t | errors: %d | warnings: %d\n", plan.LintValid, plan.ErrorCount, plan.WarningCount)
	fmt.Fprintf(&b, "Specs dir: %s\n\n", plan.SpecsDir)
	writeRunPlanSection(&b, "Safe parallel", plan.Safe)
	writeRunPlanSection(&b, "Serial only", plan.Serial)
	writeRunPlanSection(&b, "Conflict-prone", plan.Conflict)
	writeRunPlanSection(&b, "Blocked", plan.Blocked)
	writeRunPlanSection(&b, "Invalid", plan.Invalid)
	writeRunPlanSection(&b, "Active", plan.Active)
	writeRunPlanSection(&b, "Done", plan.Done)
	return b.String()
}

func writeRunPlanSection(b *strings.Builder, title string, tasks []forge.RunPlanTask) {
	fmt.Fprintf(b, "%s (%d)\n", title, len(tasks))
	if len(tasks) == 0 {
		b.WriteString("- none\n\n")
		return
	}
	for _, task := range tasks {
		fmt.Fprintf(b, "- %s :: %s", task.File, task.Task)
		if task.Repo != "" {
			fmt.Fprintf(b, " [repo:%s]", task.Repo)
		}
		b.WriteString("\n")
		if len(task.Reasons) > 0 {
			fmt.Fprintf(b, "  reasons: %s\n", strings.Join(task.Reasons, "; "))
		}
		if len(task.RelatedTasks) > 0 {
			fmt.Fprintf(b, "  related: %s\n", strings.Join(task.RelatedTasks, ", "))
		}
		if len(task.Deps) > 0 {
			fmt.Fprintf(b, "  deps: %s\n", strings.Join(task.Deps, ", "))
		}
		if len(task.Touches) > 0 {
			fmt.Fprintf(b, "  touches: %s\n", strings.Join(task.Touches, ", "))
		}
		if len(task.SharedContracts) > 0 {
			fmt.Fprintf(b, "  shared-contracts: %s\n", strings.Join(task.SharedContracts, ", "))
		}
	}
	b.WriteString("\n")
}

func formatTestSetupPlan(plan forge.TestSetupPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Forge Test Setup — %s (%s)\n", plan.ProjectName, plan.ProjectID)
	fmt.Fprintf(&b, "Spec: %s\n", plan.SpecFile)
	fmt.Fprintf(&b, "Bootstrap: %t\n", plan.Bootstrap)
	if plan.Updated {
		b.WriteString("Validation plan updated in spec: yes\n")
	}
	b.WriteString("\nDetected harnesses\n")
	if len(plan.Harnesses) == 0 {
		b.WriteString("- none\n")
	} else {
		for _, harness := range plan.Harnesses {
			features := []string{}
			if harness.HasPlaywright {
				features = append(features, "playwright")
			}
			if harness.HasCypress {
				features = append(features, "cypress")
			}
			if harness.HasVitest {
				features = append(features, "vitest")
			}
			if harness.HasJest {
				features = append(features, "jest")
			}
			if harness.HasIntegrationScript || harness.HasIntegrationFiles {
				features = append(features, "integration")
			}
			if harness.GoProject {
				features = append(features, "go-test")
			}
			if len(features) == 0 {
				features = append(features, "no-known-harness")
			}
			fmt.Fprintf(&b, "- %s (%s): %s\n", harness.RepoID, fallbackString(harness.RepoName, harness.RepoID), strings.Join(features, ", "))
		}
	}
	b.WriteString("\nValidation plan\n")
	if len(plan.Items) == 0 {
		b.WriteString("- no scenarios could be mapped yet\n")
		return b.String()
	}
	for _, item := range plan.Items {
		fmt.Fprintf(&b, "- %s [%s]\n", item.Scenario, item.RepoID)
		fmt.Fprintf(&b, "  layer: %s | runner: %s | strategy: %s\n", item.Layer, item.Runner, item.Strategy)
		if item.Command != "" {
			fmt.Fprintf(&b, "  command: %s\n", item.Command)
		}
		if len(item.CoveredBy) > 0 {
			fmt.Fprintf(&b, "  covered by: %s\n", strings.Join(item.CoveredBy, ", "))
		}
		fmt.Fprintf(&b, "  notes: %s\n", item.Reason)
	}
	return b.String()
}

func fallbackString(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func formatProjectCreatePlan(plan forge.ProjectCreatePlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Forge Project Create — %s (%s)\n", plan.ProjectName, plan.ProjectID)
	fmt.Fprintf(&b, "Pack: %s (%s)\n", plan.Pack.Name, plan.Pack.ID)
	if plan.Pack.SourceKind != "" {
		source := plan.Pack.SourceKind
		if plan.Pack.SourcePath != "" {
			source = fmt.Sprintf("%s:%s", source, plan.Pack.SourcePath)
		} else if plan.Pack.SourceName != "" {
			source = fmt.Sprintf("%s:%s", source, plan.Pack.SourceName)
		}
		fmt.Fprintf(&b, "Pack source: %s\n", source)
	}
	fmt.Fprintf(&b, "Topology: %s\n", plan.Topology.Mode)
	fmt.Fprintf(&b, "Workspace project root: %s\n", plan.ProjectRoot)
	fmt.Fprintf(&b, "Creation root: %s\n", plan.CreationRoot)
	if plan.Topology.RootRepoID != "" {
		fmt.Fprintf(&b, "Root repo: %s\n", plan.Topology.RootRepoID)
	}
	if len(plan.Repos) > 0 {
		b.WriteString("\nRepos\n")
		for _, repo := range plan.Repos {
			fmt.Fprintf(&b, "- %s (%s)\n", repo.RepoID, repo.RepoName)
			if repo.Path != "" {
				fmt.Fprintf(&b, "  path: %s\n", repo.Path)
			}
			if repo.RepoRole != "" {
				fmt.Fprintf(&b, "  role: %s\n", repo.RepoRole)
			}
		}
	}
	if len(plan.Components) > 0 {
		b.WriteString("\nComponents\n")
		for _, component := range plan.Components {
			fmt.Fprintf(&b, "- %s [%s]\n", component.ComponentID, component.Kind)
			if component.Location.Path != "" {
				fmt.Fprintf(&b, "  location: %s in repo %s at %s\n", component.Location.Type, component.Location.RepoID, component.Location.Path)
			} else {
				fmt.Fprintf(&b, "  location: %s in repo %s\n", component.Location.Type, component.Location.RepoID)
			}
			if component.Stack != "" {
				fmt.Fprintf(&b, "  stack: %s\n", component.Stack)
			}
		}
	}
	if len(plan.MetadataChanges) > 0 {
		b.WriteString("\nMetadata changes\n")
		for _, change := range plan.MetadataChanges {
			fmt.Fprintf(&b, "- %s\n", change)
		}
	}
	if len(plan.FilesystemChanges) > 0 {
		b.WriteString("\nFilesystem changes\n")
		for _, change := range plan.FilesystemChanges {
			fmt.Fprintf(&b, "- %s\n", change)
		}
	}
	if len(plan.ScaffoldFiles) > 0 {
		b.WriteString("\nStarter files\n")
		for _, file := range plan.ScaffoldFiles {
			fmt.Fprintf(&b, "- %s\n", file)
		}
	}
	if len(plan.Warnings) > 0 {
		b.WriteString("\nWarnings\n")
		for _, warning := range plan.Warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
	}
	if len(plan.NextSteps) > 0 {
		b.WriteString("\nNext steps\n")
		for _, step := range plan.NextSteps {
			fmt.Fprintf(&b, "- %s\n", step)
		}
	}
	if plan.Updated {
		b.WriteString("\nApplied: yes\n")
	}
	return b.String()
}

func formatProjectAddPlan(plan forge.ProjectAddPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Forge Project Add — %s (%s)\n", plan.ProjectName, plan.ProjectID)
	fmt.Fprintf(&b, "Current topology: %s\n", plan.CurrentTopology.Mode)
	fmt.Fprintf(&b, "Resulting topology: %s\n", plan.ResultingTopology.Mode)
	fmt.Fprintf(&b, "Component: %s [%s]\n", plan.Component.ComponentID, plan.Kind)
	if plan.As != "" {
		fmt.Fprintf(&b, "Hosting mode: %s\n", plan.As)
	}
	if plan.RequiresChoice {
		fmt.Fprintf(&b, "\nChoice required before apply: %s\n", strings.Join(plan.Choices, ", "))
		for _, step := range plan.NextSteps {
			fmt.Fprintf(&b, "- %s\n", step)
		}
		return b.String()
	}
	if plan.Repo != nil {
		fmt.Fprintf(&b, "Repo: %s (%s)\n", plan.Repo.RepoID, plan.Repo.RepoName)
		if plan.Repo.Path != "" {
			fmt.Fprintf(&b, "Repo path: %s\n", plan.Repo.Path)
		}
	} else if plan.Component.Location.Path != "" {
		fmt.Fprintf(&b, "Location: %s in repo %s at %s\n", plan.Component.Location.Type, plan.Component.Location.RepoID, plan.Component.Location.Path)
	}
	if len(plan.MetadataChanges) > 0 {
		b.WriteString("\nMetadata changes\n")
		for _, change := range plan.MetadataChanges {
			fmt.Fprintf(&b, "- %s\n", change)
		}
	}
	if len(plan.FilesystemChanges) > 0 {
		b.WriteString("\nFilesystem changes\n")
		for _, change := range plan.FilesystemChanges {
			fmt.Fprintf(&b, "- %s\n", change)
		}
	}
	if len(plan.Warnings) > 0 {
		b.WriteString("\nWarnings\n")
		for _, warning := range plan.Warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
	}
	if len(plan.NextSteps) > 0 {
		b.WriteString("\nNext steps\n")
		for _, step := range plan.NextSteps {
			fmt.Fprintf(&b, "- %s\n", step)
		}
	}
	if plan.Updated {
		b.WriteString("\nApplied: yes\n")
	}
	return b.String()
}

func formatRunExecuteResult(result forge.RunExecuteResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Forge Run %s — %s (%s)\n", strings.Title(result.Mode), result.Plan.ProjectName, result.Plan.ProjectID)
	if result.RunID != "" {
		fmt.Fprintf(&b, "Run ID: %s\n", result.RunID)
	}
	if result.RunFile != "" {
		fmt.Fprintf(&b, "Artifact: %s\n", result.RunFile)
	}
	fmt.Fprintf(&b, "Total: %d | succeeded: %d | skipped: %d | failed: %d\n\n",
		result.Summary.Total, result.Summary.Succeeded, result.Summary.Skipped, result.Summary.Failed)

	writeAttemptSection(&b, "Executed", result.Tasks, forge.RunStatusSucceeded)
	writeAttemptSection(&b, "Skipped", result.Tasks, forge.RunStatusSkipped)
	writeAttemptSection(&b, "Failed", result.Tasks, forge.RunStatusFailed)
	writeAttemptSection(&b, "Pending", result.Tasks, forge.RunStatusPending)

	return b.String()
}

func formatRunRecord(record forge.RunRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Forge Run — %s\n", record.RunID)
	fmt.Fprintf(&b, "Project: %s (%s)\n", record.ProjectName, record.ProjectID)
	fmt.Fprintf(&b, "Mode: %s | Status: %s\n", record.Mode, record.Status)
	fmt.Fprintf(&b, "Started: %s | Ended: %s\n", record.StartedAt, record.EndedAt)
	fmt.Fprintf(&b, "Total: %d | succeeded: %d | skipped: %d | failed: %d\n\n",
		record.Summary.Total, record.Summary.Succeeded, record.Summary.Skipped, record.Summary.Failed)

	for _, task := range record.Tasks {
		fmt.Fprintf(&b, "  [%s] %s :: %s", task.Status, task.SpecFile, task.Task)
		if task.Repo != "" {
			fmt.Fprintf(&b, " [repo:%s]", task.Repo)
		}
		b.WriteString("\n")
		if len(task.Reasons) > 0 {
			fmt.Fprintf(&b, "    reasons: %s\n", strings.Join(task.Reasons, "; "))
		}
		if task.Error != "" {
			fmt.Fprintf(&b, "    error: %s\n", task.Error)
		}
	}
	return b.String()
}

func writeAttemptSection(b *strings.Builder, title string, tasks []forge.TaskAttempt, status forge.RunStatus) {
	var filtered []forge.TaskAttempt
	for _, t := range tasks {
		if t.Status == status {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return
	}
	fmt.Fprintf(b, "%s (%d)\n", title, len(filtered))
	for _, task := range filtered {
		fmt.Fprintf(b, "- %s :: %s", task.SpecFile, task.Task)
		if task.Repo != "" {
			fmt.Fprintf(b, " [repo:%s]", task.Repo)
		}
		fmt.Fprintf(b, " [%s]", task.Bucket)
		b.WriteString("\n")
		if len(task.Reasons) > 0 {
			fmt.Fprintf(b, "  reasons: %s\n", strings.Join(task.Reasons, "; "))
		}
		if task.Error != "" {
			fmt.Fprintf(b, "  error: %s\n", task.Error)
		}
	}
	b.WriteString("\n")
}

func configureStackPackSources(explicit string) {
	merged := mergePathLists(explicit, os.Getenv("FORGE_STACKS_DIR"))
	if merged == "" {
		return
	}
	_ = os.Setenv("FORGE_STACKS_DIR", merged)
}

func mergePathLists(primary, secondary string) string {
	seen := map[string]bool{}
	parts := []string{}
	for _, raw := range []string{primary, secondary} {
		for _, item := range filepath.SplitList(strings.TrimSpace(raw)) {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			cleaned := filepath.Clean(item)
			if seen[cleaned] {
				continue
			}
			seen[cleaned] = true
			parts = append(parts, cleaned)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func findPackageRootOrCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	current := cwd
	for {
		if _, err := os.Stat(filepath.Join(current, "package.json")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return cwd
		}
		current = parent
	}
}
