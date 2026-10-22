# Runners and models

An agent stage runs one of these command-line agents in a tmux session on a private server (`tmux -L beltline`). Install at least one and sign it in. Settings → Tools checks them.

| Runner | Executable | Override |
| --- | --- | --- |
| Copilot CLI | `copilot` | `BELTLINE_COPILOT` |
| Pi | `pi` | `BELTLINE_PI` |
| Claude Code | `claude` | `BELTLINE_CLAUDE` |
| Codex CLI | `codex` | `BELTLINE_CODEX` |
| Cursor CLI | `cursor-agent` (from https://cursor.com/install, signed in with `cursor-agent login`) | `BELTLINE_CURSOR` |

Every runner starts with its approval prompts turned off, so a stage never stalls on a prompt. Read [security.md](security.md) before you point a belt at text other people can write.

- **Allowed models**: Settings → Allowed models discovers each runner's catalog using its configured executable and existing sign-in, without sending a prompt or starting a work session. Search and check exact identifiers to permit them; fresh settings allow none. Pi identifiers include their provider. Catalogs and permissions persist locally and are never library content. Refresh retains the last successful catalog on errors; changing the executable invalidates that runner's current choices. Agent, belt-stage and assistant model pickers offer only permitted catalog-present models, and saving or starting with a blank/disallowed model is refused. New runs copy all current model choices before admission; later permission/catalog changes neither revoke nor expand that frozen allowance. Assistant conversations likewise retain their original admission until they end. Existing pre-feature runs retain nonempty historical models and may repair a legacy blank stage once using a current allowed model.
- **Thinking** is set next to Model on the Agents page: Low, Medium, High, Max, or **The runner's default** (unset). New stages inherit it; each belt can override it next to its stage's Model. Agent Markdown files use an optional `thinking: high` frontmatter line. Starts and resumes pass `--thinking` to Pi, `--reasoning-effort` to Copilot, `--effort` to Claude or `-c model_reasoning_effort=…` to Codex; unset levels pass no flag, and manual session continuation leaves the level alone. Codex takes Low to High, and Cursor has no thinking setting: it uses the model's reasoning settings, so Thinking is off for it and not passed, and a saved level is kept for when you switch back. The others take every level. A level the runner does not take blocks saving the belt or agent; one that reaches a run anyway, as the assistant's setting or through an import, passes the highest level the runner takes below it. A Claude stage runs headless (`claude -p`) with `--allow-dangerously-skip-permissions --permission-mode bypassPermissions`, so no trust or permission prompt stalls it, and its exit code says when it is done; its transcript reports errors in Claude's own words, and if the output file lacks an outcome, its last main-session reply supplies it. Answering a held stage resumes the same Claude session headless; **Continue** opens it in Claude's interactive interface. A Codex stage runs headless too (`codex exec --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check`), outside Codex's sandbox so it can commit in a worktree and push; answering a held stage resumes the checkout's newest Codex session with `codex exec resume`, or starts a fresh one when there is none, and **Continue** opens `codex resume` interactively. A Cursor stage runs headless (`cursor-agent --print --force --trust`) in a chat Beltline creates first with `cursor-agent create-chat` and saves on the attempt, so answering a held stage, **Continue** and a restart of the daemon all resume that chat; Cursor keeps no transcript Beltline reads, so its outcome must be in the output file, and it reports no usage.

## Live checks against the real CLIs

These opt-in tests aren't part of `make check`. Run them from `daemon/`. Each is skipped, and makes no outside calls, unless its variable is set:

- **Claude:** a triage, `BELTLINE_REAL_CLAUDE=1 BELTLINE_REAL_CLAUDE_MODEL=haiku go test -tags integration ./internal/kit/coordinator -run TestRealClaude`.
- **Cursor:** `BELTLINE_REAL_CURSOR=1 BELTLINE_REAL_CURSOR_MODEL=<model> … -run TestRealCursor`.
- **Codex:** a triage run and resuming the checkout's own session, `BELTLINE_REAL_CODEX=1 … -run TestRealCodex`. `BELTLINE_REAL_CODEX_MODEL` picks a model.

[AGENTS.md](../AGENTS.md) lists the others.
