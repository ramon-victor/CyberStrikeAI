# Fork synchronization

This fork incorporates upstream `557f74d747bf622eda2ea74d84bee0894d476ebf` (release configuration v1.7.24), including asynchronous conversation runs, context checkpoints, project continuation, token analytics, vulnerability retesting, Word reports, installation updates and plugin RBAC login.

Operational prompts, default interface text and HTML fallbacks, logs, API descriptions and errors are English. Chinese locale resources, documentation, input aliases and historical record parsers remain available. User data and provider responses retain their original content.

The default installation update source is `https://github.com/ramon-victor/CyberStrikeAI.git`. Set `update.repo` to explicitly select another source. Local Docker settings, storage mounts, port mappings, tool installation and OpenAI retry behavior are retained. Docker builds the complete `cmd/server` package.

The default fact-index budget is 12,000 characters so translated continuation instructions and saved project results fit together. Explicit `project.fact_index_max_runes` settings remain authoritative; the supplied configurations use 65,000.

Before another upstream merge, review both parents of every conflict. Translate operational content while preserving placeholders, tool names, API keys, query syntax and compatibility with stored records. Do not translate command aliases or other values used to recognize historical input.

Validation:

- `go test ./internal/... ./cmd/...` covers project packages without traversing runtime workspace directories.
- `node --test web/static/js/*.test.cjs plugins/browser-extension/cyberstrikeai-browser-extension/tests/*.test.cjs` covers frontend behavior and browser-extension authentication.
- `go build ./cmd/server`, the Docker `app-builder` target and both Compose configurations validate distribution.
- Rebuild the browser ZIP with `package.sh` and the Burp JAR with `build-mvn.sh` after changing their sources.
- Verify startup, login, API responses and visible UI text with an isolated temporary database/configuration. Never migrate a live database merely to test a merge.

Regression tests cover English prompts and HTML fallbacks, English/legacy error status classification, custom headers on streaming retries and avoidance of replay after partial streaming output.
