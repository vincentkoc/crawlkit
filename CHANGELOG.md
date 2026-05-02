# Changelog

## Unreleased

- Initial `crawlkit` module scaffold.
- Add `tui`, a shared Bubble Tea terminal archive browser used by the crawl apps for consistent `tui` command behavior.
- Improve `tui` rows with compact column rendering, pane-specific scrolling, and full-height pane borders.
- Tune `tui` pane colors and mouse-wheel buffering to better match the `gitcrawl` terminal browser feel.
- Add shared `tui` explorer controls: mouse row selection, pane-aware right-click menus, help/sort menus, and stable sorting by time/title/kind/scope/container/author.
- Align `tui` pane chrome with `gitcrawl`: wide three-column layout, split/stacked resize modes, focused pane titles, compact row headers, click-to-sort headers, and floating right-click menus.
- Make the shared `tui` explorer group-aware: left pane now shows channels/people or document parents, middle pane shows group members, and right pane shows detail/thread content.
- Rename the public package nouns to `config`, `store`, `snapshot`, `mirror`, `state`, `output`, `tui`, and `cache`.
