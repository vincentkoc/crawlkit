# Publishing

Go modules are published from git tags. There is no separate registry upload.

## First release

```bash
git tag v0.1.0
git push origin main
git push origin v0.1.0
```

Consumers can then use:

```bash
go get github.com/vincentkoc/crawlkit@v0.1.0
```

`pkg.go.dev` indexes public modules automatically after the tag is reachable.

## Versioning

Keep `v0.x.y` while the downstream crawler rewires are still settling. If the
module ever reaches `v2`, Go requires the module path to become:

```text
github.com/vincentkoc/crawlkit/v2
```

