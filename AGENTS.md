# uirefs: agent rules

Read `docs/design.md` in full before you change anything. It is the contract: scope, data model, MCP tools, web UI, packaging and acceptance list. Work briefs and reports are local, untracked files in `docs/briefs/` and `docs/reports/`.

- **Language.** Go, standard library plus `modernc.org/sqlite`. `CGO_ENABLED=0` always: the product is one static binary. No new runtime dependency without a written reason in your report.
- **Web UI.** Plain HTML, CSS and JS in `web/`, embedded with `go:embed`. No build step, no framework.
- **Commands.** `just check` (vet + tests), `just build`, `just dev`, `just worktree <name>`. Go comes from `mise.toml`; run `mise install` in a fresh checkout if `go` is not found.
- **Verification.** Run the service from your worktree with a temporary `--data` dir and a free port on 127.0.0.1. Never touch other services' ports or the real data dir. Stop what you start.
- **Tests.** Meaningful behavior only: type rules, search ranking, delete, MCP contracts, the demo sandbox. No tests for constants, layout or framework behavior.
- **Minimal.** The smallest cohesive diff that meets the acceptance list. No speculative abstractions, no extra features.
- **Git.** Commit on your branch in coherent steps. No push, no merge into `main`.
- **Skills.** `skills/` holds agent skills for operating the library (today `ui-reference-ingestion`: how to get the code, or a recreation, of a reference's demo and save it). The usage guide that every agent reads stays in `internal/guide/guide.md`, served by the MCP.
- **Reports.** Write your final report to the file named in your brief (under `docs/reports/`, which git ignores): SHA, commands run with real results, what is unverified, what is left running.
