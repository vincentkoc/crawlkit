# Publishing Crawlkit

Go modules are published from git tags. There is no separate registry upload.

## Release checklist

1. Rebase `crawlkit` and every downstream app branch on each repo's `origin/main`.
2. Run the crawlkit gate:

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go vet ./...
go test ./...
```

3. Test downstream apps against the local checkout through a temporary Go workspace.
4. Merge `crawlkit` to `main`.
5. Tag the next semver release from `main`:

```bash
git tag -s v0.4.0
git push origin main
git push origin v0.4.0
```

6. Prime and verify module proxy visibility:

```bash
GOPROXY=https://proxy.golang.org go list -m github.com/vincentkoc/crawlkit@v0.4.0
go list -m github.com/vincentkoc/crawlkit@v0.4.0
```

7. Bump downstream apps to the new tag and commit their `go.mod`/`go.sum` updates:

```bash
go get github.com/vincentkoc/crawlkit@v0.4.0
go mod tidy
```

`pkg.go.dev` indexes public modules automatically after the tag is reachable.

Use a patch tag such as `v0.3.17` only for narrow bug fixes on the existing API.
Use a minor tag such as `v0.4.0` for broad shared TUI or crawler infrastructure
changes. This branch is a `v0.4.0`-shaped release.

## Versioning

Keep `v0.x.y` while the downstream crawler rewires are still settling. If the
module ever reaches `v2`, Go requires the module path to become:

```text
github.com/vincentkoc/crawlkit/v2
```
