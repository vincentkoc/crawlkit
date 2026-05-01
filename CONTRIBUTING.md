# Contributing

## Development

```bash
go test ./...
go vet ./...
go mod tidy
git diff --exit-code -- go.mod go.sum
```

Keep public APIs small. `crawlkit` should own reusable local archive mechanics,
not provider-specific Slack, Discord, Notion, or GitHub behavior.

## Compatibility

This module starts at `v0`, so APIs may still change. Prefer additive changes
and keep downstream crawler rewires narrow.

