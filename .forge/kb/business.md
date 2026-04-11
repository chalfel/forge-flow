# Business — Forge Flow

## What it is
Forge Flow is a spec-driven AI agent orchestrator. It plans, spawns, tracks, and reviews work done by Claude agents across one or more repositories, using capability specs as the single source of truth.

The operator writes specs. Forge breaks them into parallelizable tasks, spawns agents in git worktrees inside tmux sessions, and surfaces "what needs me?" through an agentic board.

## Core philosophy
**The board is an agentic surface, not a project management tool.**

- The operator co-pilots with AI agents. The human exercises judgment; agents operate.
- Every UI element answers "does the operator need to do something here?"
- Prefer "Needs attention" aggregation over scattered status pages.
- Running agents are interactive — the operator can join a tmux session and talk to the agent mid-task.
- The board tells the operator what to do next (Focus mode), not just what's happening.
- Activity feed is the heartbeat — agents working in real-time.
- Design target: **open board → 5s to understand → 30s to act → back to flow.**

## Who uses it
- **Solo builders** running multi-repo SaaS or tooling projects who want AI agents to do the tedious work in parallel without losing coherence.
- **Small teams** that want a shared spec-driven workflow where AI agents pick up ready tasks automatically.

## Revenue model
Open source. No monetization plan at the moment — the value is in speeding up the operator, not in charging for the tool.

## Core product rules
- **Specs are the source of truth.** Agents read specs; agents update task metadata. Nothing else drives the state machine.
- **One spec = one capability, shippable, demo-able.** Every spec must have a `Demo:` line describing what changes from a user's perspective.
- **Tasks are parallelizable by default.** Unless they touch the same files or share contracts, agents can run them in parallel in separate worktrees.
- **The first task of a new module is always a serial scaffolding blocker.** Prevents parallel worktrees from creating the same files independently.
- **Memory is promoted, not auto-saved.** Agents propose memories to `inbox.md`; humans promote them to `kb/memory.md`.
- **Agents must respect the protocol.** Events go to `$FORGE_EVENTS_FILE`. Decisions are explicit. Blockers and `need_info` are first-class — don't silently fail.

## Personas

### "Caio" — solo founder
Runs 3–5 parallel repos (backend, web, mobile, admin). Wants to define capabilities, walk away, and come back to find PRs ready for review or clear signals about what needs attention.
- Pain: context-switching between repos + agents + terminals destroys flow state.
- Win: single board that tells him exactly what to do in 5 seconds.

### "Agent operator" — ML/AI-savvy builder
Spawns multiple Claude sessions per day. Tired of tmux soup and lost agent output.
- Pain: no unified view of agent progress; lost in terminal windows.
- Win: real-time activity feed + agent status bar + one-click session join.
