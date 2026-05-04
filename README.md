# crawlkit

Shared Go infrastructure for local-first crawler archives.

`crawlkit` is not a universal Slack, Discord, Notion, or GitHub crawler. It is
the reusable foundation beneath those tools: SQLite hygiene, TOML config
defaults, portable JSONL/Gzip packing, git-backed snapshot sharing, sync state,
CLI output helpers, a shared terminal explorer, and safe desktop-cache snapshot
utilities.

## Install

```bash
go get github.com/vincentkoc/crawlkit@latest
```

Go packages are published by tagging this repository. There is no separate
package registry step. See `docs/publishing.md` for the release commands.

## Packages

- `config`: standard TOML config paths, runtime dirs, and token diagnostics.
- `store`: SQLite open/read-only/transaction/query helpers.
- `snapshot`: `manifest.json` plus JSONL/Gzip table snapshot export and import.
- `mirror`: clone/init/pull/commit/push helpers for private snapshot repos.
- `state`: generic crawler cursor and freshness records.
- `output`: text/json/log output helpers.
- `tui`: shared terminal archive explorer with gitcrawl-style responsive panes, entity/member/detail lanes, compact sortable headers, mouse selection, floating right-click actions, sorting/filtering, and local/remote source status.
- `cache`: safe read-only local cache snapshot helpers.

## Safety

Library tests use temporary directories. They do not touch app runtime stores
such as `~/.config/gitcrawl`, `~/.slacrawl`, `~/.discrawl`, or `~/.notcrawl`.
