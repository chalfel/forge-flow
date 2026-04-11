# Forge Constraints (non-negotiable)

These rules apply to every Forge agent spawned in this project. Agents must follow them regardless of task instructions.

## Git
- NEVER force push
- NEVER push directly to main or master
- Always commit to the task's branch inside the worktree
- Always create a PR via `gh pr create` before exiting

## Security
- NEVER commit secrets (API keys, passwords, tokens, private keys)
- NEVER run destructive commands outside the worktree (rm -rf, drop table, truncate)
- NEVER access files outside the worktree or its declared `touches:` metadata

## Quality
- Always run the project's lint command before declaring the task done
- Always ensure tests pass
- Never leave commented-out dead code in the diff

## Escalation
- If blocked, write an event to $FORGE_EVENTS_FILE with type "blocked" and exit
- If you need operator input, write an event with type "need_info" and exit
- Do not loop indefinitely on a task you cannot complete
