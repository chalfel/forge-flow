# Forge Stack Packs

Stack packs power `forge project create`.

## Contract

Each pack lives in its own directory:

```text
stacks/
  <pack-id>/
    stack.json
```

`stack.json` supports this MVP contract:

- `id` — stable pack id
- `name` — human-readable name
- `description` — short summary shown in plans/docs
- `defaultTopology` — default project topology
- `supportedTopologies` — allowed values: `single-repo`, `monorepo`, `multi-repo`
- `rootRepo` — defaults for a root repo when the chosen topology needs one
- `qualityExpectations` — starter quality rules copied into shared project docs
- `starterSteps` — next-step guidance shown after planning/creation
- `components[]` — logical starter surfaces
  - `componentId`
  - `name`
  - `kind`
  - `role`
  - `stack`
  - `hosts[topology]`
    - `type` — `repo`, `path`, `package`, or `module`
    - `repoId`
    - `path` for non-repo locations
    - `repoNameTemplate`
    - `repoRole`
  - `scaffoldFiles` — files written relative to the resolved component root

## Built-in packs

- `blank` — shared Forge structure with minimal assumptions
- `nextjs-saas` — starter customer-facing Next.js app with Forge docs/defaults

## Custom and local packs

Forge can also discover packs outside the built-ins.

### Local pack root

A local pack root follows the same directory layout:

```text
/my-local-packs/
  custom-web/
    stack.json
  my-monorepo/
    stack.json
```

List packs from a local root explicitly:

```bash
forge project create --list-packs --packs-dir /my-local-packs
```

Create a project from a local pack explicitly:

```bash
forge project create --name "Acme Platform" --pack custom-web --packs-dir /my-local-packs
```

You can also provide one or more roots through `FORGE_STACKS_DIR` using the OS path-list separator.

### Workspace-shared packs

A workspace can publish shared packs at:

```text
~/.forge/workspaces/<workspace>/shared/stacks/<pack-id>/stack.json
```

That lets one Forge workspace share opinionated packs across multiple repos/projects on the same machine.

## Source precedence

When duplicate pack ids exist, Forge resolves them in this order:

1. `--packs-dir` roots passed to the CLI, left to right
2. `FORGE_STACKS_DIR` roots, left to right
3. workspace shared packs
4. built-in packs

Use `forge project create --list-packs --json` to inspect `sourceKind`, `sourceName`, and `sourcePath`.

## Safety guidance

- Prefer unique pack ids unless you are intentionally overriding another source.
- When testing or overriding a pack, prefer `--packs-dir` over ambient environment defaults so the source choice is explicit.
- If a custom pack is invalid, Forge fails before project creation starts.
- Do not rely on silent shadowing; inspect the resolved source before creating a real project.

## Direction

This is intentionally small and deterministic for the MVP:
- JSON contract first
- built-in packs first
- CLI planning/apply first
- richer templating and external/community packs later
