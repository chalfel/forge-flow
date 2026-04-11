# Roadmap — Forge Flow

## Now (shipping)
- **Agentic board surface** (done 2026-04-11)
  - Activity feed, operator inbox, focus mode, agent status, SSE, keyboard nav
  - Answers "what needs me?" in 5 seconds
- **Meta-forge** (in progress)
  - Use Forge to plan/build Forge itself. KB + specs for forge-flow.

## Next (next sprint)
- **Inbox actions** — make inbox items actionable from the board:
  - Respond to `need_info` via a textarea that writes to the agent's events file
  - Approve / request changes on `in_review` tasks without leaving the board
  - Promote proposed memories to `kb/memory.md` with one click
  - Unblock tasks by marking deps done
- **Agent tmux join** — click an active agent → get the exact `tmux attach` command (later: embedded terminal in the board)
- **Run history detail page** — drill into any run's events, decisions, and task attempts. Timeline view with filters.

## Later
- **Native app (Tauri)** — wraps the board in a native window with embedded terminal. Replaces the web UI as the primary operator surface.
- **Multi-project board** — see specs across multiple projects in one view.
- **Spec creation from board** — author specs interactively without leaving the UI.
- **Watch mode UI integration** — start/stop the watch daemon from the board, not just the CLI.
- **Search/filter everywhere** — full-text search across specs, tasks, events, decisions.

## Vision
Forge Flow becomes the default operator surface for anyone running AI agents on their code. The operator writes intent, agents execute, the board tells you what needs judgment. Flow state by default.

The board should feel like a cockpit: alarms when something needs you, calm when it doesn't, always showing the one next action.
