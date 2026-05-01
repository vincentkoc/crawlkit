# crawlkit

Shared Go infrastructure for local-first crawler archives.

`crawlkit` is not a universal Slack, Discord, Notion, or GitHub crawler. It is
the reusable foundation beneath those tools: SQLite hygiene, TOML config
defaults, portable JSONL/Gzip packing, git-backed snapshot sharing, sync state,
CLI output helpers, and safe desktop-cache snapshot utilities.

## Install

```bash
go get github.com/vincentkoc/crawlkit@v0.1.0
```

Go packages are published by tagging this repository. There is no separate
package registry step.

## Packages

- `configkit`: standard TOML config paths, runtime dirs, and token diagnostics.
- `sqlitekit`: SQLite open/read-only/transaction/query helpers.
- `pack`: `manifest.json` plus JSONL/Gzip table snapshot export and import.
- `gitshare`: clone/init/pull/commit/push helpers for private snapshot repos.
- `syncstate`: generic crawler cursor and freshness records.
- `cliout`: text/json/log output helpers.
- `desktopcache`: safe read-only local cache snapshot helpers.

## Safety

Library tests use temporary directories. They do not touch app runtime stores
such as `~/.config/gitcrawl`, `~/.slacrawl`, `~/.discrawl`, or `~/.notcrawl`.

