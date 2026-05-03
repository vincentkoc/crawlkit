package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func stripANSI(value string) string {
	return regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`).ReplaceAllString(value, "")
}

func testDetailLines(count int) string {
	lines := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	return strings.Join(lines, "\n")
}

func TestBrowseJSONUsesUniversalRows(t *testing.T) {
	var out bytes.Buffer
	rows := []Row{{
		Source:    "slack",
		Kind:      "message",
		ID:        "C1/123",
		Scope:     "T1",
		Container: "general",
		Author:    "vincent",
		Title:     "ship it",
		Text:      "ship crawlkit tui",
		CreatedAt: "2026-05-01T12:00:00Z",
		Fields:    map[string]string{"thread": "123"},
	}}
	if err := Browse(context.Background(), BrowseOptions{AppName: "slacrawl", Rows: rows, JSON: true, Stdout: &out}); err != nil {
		t.Fatalf("Browse json: %v", err)
	}
	var decoded []Row
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode json: %v\n%s", err, out.String())
	}
	if len(decoded) != 1 || decoded[0].Source != "slack" || decoded[0].Kind != "message" || decoded[0].Title != "ship it" {
		t.Fatalf("decoded rows = %#v", decoded)
	}
}

func TestBrowseJSONEncodesNilRowsAsEmptyArray(t *testing.T) {
	var out bytes.Buffer
	if err := Browse(context.Background(), BrowseOptions{AppName: "discrawl", JSON: true, Stdout: &out}); err != nil {
		t.Fatalf("Browse json: %v", err)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("json = %q, want []", out.String())
	}
}

func TestRowItemUsesSharedArchiveShape(t *testing.T) {
	item := Row{
		Source:    "discord",
		Kind:      "message",
		ID:        "m1",
		Scope:     "@me",
		Container: "dm",
		Author:    "sam",
		Title:     "panic locked database",
		Text:      "full message text",
		Fields:    map[string]string{"reply_to": "m0"},
	}.ItemForLayout(LayoutList)
	if item.Title != "panic locked database" {
		t.Fatalf("title = %q", item.Title)
	}
	if !strings.Contains(item.Subtitle, "@me") || !strings.Contains(item.Subtitle, "dm") || !strings.Contains(item.Subtitle, "sam") {
		t.Fatalf("subtitle = %q", item.Subtitle)
	}
	if !strings.Contains(item.Detail, "full message text") || !strings.Contains(item.Detail, "reply_to=m0") {
		t.Fatalf("detail = %q", item.Detail)
	}
	if len(item.Tags) < 2 || item.Tags[0] != "discord" || item.Tags[1] != "message" {
		t.Fatalf("tags = %#v", item.Tags)
	}
}

func TestChatLayoutIndentsReplyRows(t *testing.T) {
	item := Row{
		Source:    "discord",
		Kind:      "message",
		ID:        "m2",
		ParentID:  "m1",
		Container: "general",
		Author:    "sam",
		Title:     "reply",
	}.ItemForLayout(LayoutChat)
	if item.Depth != 1 {
		t.Fatalf("depth = %d", item.Depth)
	}
	if strings.Contains(item.Subtitle, "discord") {
		t.Fatalf("chat subtitle should prioritize chat context, got %q", item.Subtitle)
	}
}

func TestRowsPaneUsesCompactTitlesAndKeepsMetadataInContext(t *testing.T) {
	m := newModel(Options{
		Title: "discrawl archive",
		Items: []Item{Row{
			Source:    "discord",
			Kind:      "message",
			ID:        "m1",
			Container: "general",
			Author:    "vincent",
			Title:     strings.Repeat("long message ", 30),
			Text:      strings.Repeat("long message ", 30),
			CreatedAt: "2026-05-02T12:00:00Z",
		}.ItemForLayout(LayoutChat)},
	})
	m.width = 100
	m.height = 18
	view := m.View()
	if !strings.Contains(view, "Messages") || !strings.Contains(view, "general") {
		t.Fatalf("context pane should render grouped messages:\n%s", view)
	}
	if !strings.Contains(view, "Message") || !strings.Contains(view, "general") || !strings.Contains(view, "vincent") {
		t.Fatalf("detail pane should render chat-style message detail:\n%s", view)
	}
}

func TestRowsPaneUsesStableColumns(t *testing.T) {
	line := rowListLine(Item{
		Title:    "Can you check again? Hoping this update worked.",
		Subtitle: "general  vincent  2026-05-02T12:00:00Z",
		Tags:     []string{"message", "discord"},
	}, 100)
	for _, want := range []string{"message", "2026-05-02", "general", "Can you check"} {
		if !strings.Contains(line, want) {
			t.Fatalf("row line missing %q: %q", want, line)
		}
	}
	if strings.Contains(line, "vincent  2026") {
		t.Fatalf("row line should not dump raw subtitle: %q", line)
	}
}

func TestViewUsesGitcrawlStylePaneTables(t *testing.T) {
	m := newModel(Options{
		Title:  "slacrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "one", Scope: "T1", Container: "general", Author: "Amy", Title: "first update", CreatedAt: "2026-05-02T09:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "two", Scope: "T1", Container: "general", Author: "Zed", Title: "second update", CreatedAt: "2026-05-02T10:00:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	m.width = 300
	m.height = 28
	view := stripANSI(m.View())
	for _, want := range []string{"type", "count", "latest", "scope", "group", "kind", "time", "where", "author", "title"} {
		if !strings.Contains(view, want) {
			t.Fatalf("table view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "> general") || strings.Contains(view, "> first update") {
		t.Fatalf("pane tables should use row styling instead of prompt prefixes:\n%s", view)
	}
}

func TestWideRenderFillsTerminalAndKeepsThreePaneColumns(t *testing.T) {
	m := newModel(Options{
		Title:  "discrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "one", Scope: "guild", Container: "general", Author: "Amy", Title: "first update", CreatedAt: "2026-05-02T09:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "two", Scope: "guild", Container: "general", Author: "Zed", Title: "second update", CreatedAt: "2026-05-02T10:00:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	m.width = 220
	m.height = 34
	view := stripANSI(m.View())
	lines := strings.Split(view, "\n")
	if len(lines) != 34 {
		t.Fatalf("rendered height = %d, want 34:\n%s", len(lines), view)
	}
	if len(lines[0]) != 220 || len(lines[len(lines)-1]) != 220 {
		t.Fatalf("view did not fill terminal width: first=%d last=%d\n%s", len(lines[0]), len(lines[len(lines)-1]), view)
	}
	for _, want := range []string{"Channels", "Messages", "Thread", "type", "count", "latest", "age", "scope", "group", "kind", "time", "where", "author", "title"} {
		if !strings.Contains(view, want) {
			t.Fatalf("wide render missing %q:\n%s", want, view)
		}
	}
}

func TestCompactWidthKeepsUsefulColumns(t *testing.T) {
	group := itemGroup{Kind: "channel", Count: 18, Latest: "2026-05-02T12:00:00Z", Title: "github-secure-session-4"}
	groupHeader := groupListHeader(40, sortDefault)
	groupLine := groupListLine(group, 40)
	for _, want := range []string{"N", "AGE", "GROUP", "18", "github-secure"} {
		if !strings.Contains(groupHeader+groupLine, want) {
			t.Fatalf("compact group columns missing %q:\n%s\n%s", want, groupHeader, groupLine)
		}
	}

	rowHeader := rowListHeader(42, sortDefault)
	rowLine := rowListLine(Item{
		Title:     "Im working on adding",
		Author:    "Vincent Koc",
		CreatedAt: "2026-05-02T12:00:00Z",
	}, 42)
	for _, want := range []string{"TIME", "AGE", "WHO", "TITLE", "05-02", "Vinc", "Im working"} {
		if !strings.Contains(rowHeader+rowLine, want) {
			t.Fatalf("compact row columns missing %q:\n%s\n%s", want, rowHeader, rowLine)
		}
	}
}

func TestVeryNarrowPanesStillShowCompactColumns(t *testing.T) {
	group := itemGroup{Kind: "channel", Count: 18, Latest: "2026-05-02T12:00:00Z", Title: "github-secure-session-4"}
	groupHeader := groupListHeader(28, sortDefault)
	groupLine := groupListLine(group, 28)
	for _, want := range []string{"N", "AGE", "GROUP", "18", "github-secure"} {
		if !strings.Contains(groupHeader+groupLine, want) {
			t.Fatalf("narrow group columns missing %q:\n%s\n%s", want, groupHeader, groupLine)
		}
	}

	rowHeader := rowListHeader(28, sortDefault)
	rowLine := rowListLine(Item{
		Title:     "Im working on adding",
		Author:    "Vincent Koc",
		CreatedAt: "2026-05-02T12:00:00Z",
	}, 28)
	for _, want := range []string{"TIME", "TITLE", "05-02", "Im working"} {
		if !strings.Contains(rowHeader+rowLine, want) {
			t.Fatalf("narrow row columns missing %q:\n%s\n%s", want, rowHeader, rowLine)
		}
	}
}

func TestQQuitsFromMenuAndFilterModes(t *testing.T) {
	m := newModel(Options{Title: "archive", Items: []Item{{Title: "alpha"}}})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = updated.(model)
	if !m.menuOpen {
		t.Fatal("menu did not open")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q in menu should quit")
	}

	m = newModel(Options{Title: "archive", Items: []Item{{Title: "alpha"}}})
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(model)
	if !m.filterMode {
		t.Fatal("filter did not start")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q in filter should quit")
	}
}

func TestInitialTerminalSizeCanUseTallPane(t *testing.T) {
	m := newModel(Options{Title: "archive", Items: []Item{{Title: "alpha"}}})
	m.width = 84
	m.height = 60
	view := m.View()
	if got := strings.Count(view, "\n") + 1; got != 60 {
		t.Fatalf("view height = %d, want 60", got)
	}
}

func TestChatDetailUsesTranscriptShapeBeforeMetadata(t *testing.T) {
	m := newModel(Options{
		Title:  "slacrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "m1", Container: "general", Author: "alice", Title: "root", Text: "root message", CreatedAt: "2026-05-01T10:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "m2", ParentID: "m1", Container: "general", Author: "bob", Title: "reply", Text: "reply message", CreatedAt: "2026-05-01T10:01:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	m.selectItemIndex(1)
	item, ok := m.selectedItem()
	if !ok {
		t.Fatal("missing selected item")
	}
	lines := m.detailLines(item)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"general  bob", "Thread", "alice", "root message", "> bob", "reply message", "Properties", "IDs", "parent=m1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("chat detail missing %q:\n%s", want, joined)
		}
	}
	if strings.Index(joined, "Thread") > strings.Index(joined, "Properties") {
		t.Fatalf("chat detail should put readable content before properties:\n%s", joined)
	}
}

func TestChatDetailRendersMarkdownTranscriptLikeGitcrawl(t *testing.T) {
	m := newModel(Options{
		Title:  "discrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "m1", Container: "general", Author: "alice", Title: "root", Text: "# Plan\n- ship columns\n- polish [preview](https://example.com)", CreatedAt: "2026-05-01T10:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "m2", ParentID: "m1", Container: "general", Author: "bob", Title: "reply", Text: "> agreed\n`done`", CreatedAt: "2026-05-01T10:01:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	m.selectItemIndex(1)
	item, ok := m.selectedItem()
	if !ok {
		t.Fatal("missing selected item")
	}
	joined := stripANSI(strings.Join(m.detailLinesForWidth(item, 52), "\n"))
	for _, want := range []string{"Plan", "- ship columns", "polish preview <https://example.com>", "> agreed", "done", "Properties", "IDs"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("markdown chat detail missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "# Plan") || strings.Contains(joined, "`done`") {
		t.Fatalf("chat detail should render markdown-ish text, not raw markdown:\n%s", joined)
	}
}

func TestDetailModeToggleUsesCompactReadableDetail(t *testing.T) {
	m := newModel(Options{
		Title:  "discrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "m1", Container: "general", Author: "alice", Title: "root", Text: "root message", CreatedAt: "2026-05-01T10:00:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	full := stripANSI(strings.Join(m.detailLinesForWidth(m.items[0], 60), "\n"))
	if !strings.Contains(full, "Properties") || !strings.Contains(full, "IDs") {
		t.Fatalf("full detail should include metadata sections:\n%s", full)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	if !m.compactDetail {
		t.Fatal("detail mode did not toggle to compact")
	}
	compact := stripANSI(strings.Join(m.detailLinesForWidth(m.items[0], 60), "\n"))
	if !strings.Contains(compact, "root message") || strings.Contains(compact, "Properties") || strings.Contains(compact, "IDs") {
		t.Fatalf("compact detail should keep readable content and hide metadata sections:\n%s", compact)
	}
	if !strings.Contains(stripANSI(m.View()), "d detail") {
		t.Fatalf("footer should expose detail toggle:\n%s", stripANSI(m.View()))
	}
}

func TestChatMembersDefaultToChronologicalTranscriptOrder(t *testing.T) {
	m := newModel(Options{
		Title:  "slacrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "new", Container: "general", Author: "bob", Title: "new", CreatedAt: "2026-05-01T10:02:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "old", Container: "general", Author: "alice", Title: "old", CreatedAt: "2026-05-01T10:00:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	members := m.currentGroupMembers()
	if len(members) != 2 {
		t.Fatalf("members = %#v", members)
	}
	if got := m.items[members[0]].ID; got != "old" {
		t.Fatalf("first member = %q, want oldest message first", got)
	}
	m.setMemberSortMode(sortNewest)
	members = m.currentGroupMembers()
	if got := m.items[members[0]].ID; got != "new" {
		t.Fatalf("newest sort first member = %q, want newest message first", got)
	}
}

func TestChatMembersScopeSortUsesScopeNotContainer(t *testing.T) {
	m := newModel(Options{
		Title:  "slacrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "one", Scope: "z-workspace", Container: "general", Title: "one"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "two", Scope: "a-workspace", Container: "general", Title: "two"}.ItemForLayout(LayoutChat),
		},
	})
	m.setMemberSortMode(sortScope)
	members := m.currentGroupMembers()
	if len(members) != 2 {
		t.Fatalf("members = %#v", members)
	}
	if got := m.items[members[0]].ID; got != "two" {
		t.Fatalf("scope-sorted first member = %q, want a-workspace row", got)
	}
}

func TestFocusedDetailPaneScrollsIndependently(t *testing.T) {
	m := newModel(Options{
		Title: "discrawl archive",
		Items: []Item{{
			Title:  "first",
			Detail: testDetailLines(40),
			Tags:   []string{"message", "discord"},
		}},
	})
	m.width = 80
	m.height = 24
	m.focus = focusDetail
	m.scrollFocused(1)
	if m.selected != 0 {
		t.Fatalf("detail scroll moved row selection to %d", m.selected)
	}
	if m.detailView.YOffset == 0 {
		t.Fatal("detail pane did not scroll")
	}
	view := m.View()
	if !strings.Contains(stripANSI(view), "line 02") {
		t.Fatalf("detail pane did not render scrolled content:\n%s", view)
	}
}

func TestMouseWheelBurstsAreBuffered(t *testing.T) {
	items := make([]Item, 0, 30)
	for i := 0; i < 30; i++ {
		items = append(items, Item{Title: fmt.Sprintf("row %02d", i), Tags: []string{"message"}})
	}
	m := newModel(Options{Title: "archive", Items: items})
	m.width = 100
	m.height = 14
	layout := m.layout()
	initialSelected := m.selected

	queued := 0
	for i := 0; i < 40; i++ {
		updated, cmd := m.Update(tea.MouseMsg{
			X:      layout.rows.x + 2,
			Y:      layout.rows.y + 2,
			Type:   tea.MouseWheelDown,
			Action: tea.MouseActionPress,
			Button: tea.MouseButtonWheelDown,
		})
		m = updated.(model)
		if cmd != nil {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("wheel burst queued %d frame ticks, want 1", queued)
	}
	if m.selected != initialSelected {
		t.Fatalf("wheel burst moved immediately to %d, want %d", m.selected, initialSelected)
	}
	if m.wheelDelta != wheelMaxBufferedDelta {
		t.Fatalf("wheel burst delta = %d, want capped %d", m.wheelDelta, wheelMaxBufferedDelta)
	}
	updated, _ := m.Update(wheelScrollMsg{seq: m.wheelSeq})
	m = updated.(model)
	wantSelected := clampInt(initialSelected+wheelMaxBufferedDelta, 0, len(m.filtered)-1)
	if m.selected != wantSelected {
		t.Fatalf("wheel burst selected = %d, want capped movement to %d", m.selected, wantSelected)
	}
}

func TestKeyInputCancelsQueuedWheel(t *testing.T) {
	items := make([]Item, 0, 10)
	for i := 0; i < 10; i++ {
		items = append(items, Item{Title: fmt.Sprintf("row %02d", i), Tags: []string{"message"}})
	}
	m := newModel(Options{Title: "archive", Items: items})
	updated, cmd := m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	m = updated.(model)
	if cmd == nil || !m.wheelPending {
		t.Fatal("wheel should queue before key input")
	}
	seq := m.wheelSeq
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(model)
	if m.wheelPending || m.wheelDelta != 0 {
		t.Fatalf("key input did not cancel queued wheel: pending=%v delta=%d", m.wheelPending, m.wheelDelta)
	}
	updated, _ = m.Update(wheelScrollMsg{seq: seq})
	m = updated.(model)
	if m.selected != 1 {
		t.Fatalf("stale wheel changed selection after key input, selected=%d", m.selected)
	}
}

func TestMouseWheelTargetsPaneUnderPointer(t *testing.T) {
	m := newModel(Options{
		Title: "discrawl archive",
		Items: []Item{{
			Title:  "first",
			Detail: testDetailLines(40),
			Tags:   []string{"message", "discord"},
		}},
	})
	m.width = 100
	m.height = 24
	layout := m.layout()
	updated, cmd := m.Update(tea.MouseMsg{
		X:      layout.detail.x + 2,
		Y:      layout.detail.y + 2,
		Type:   tea.MouseWheelDown,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("detail wheel should queue a buffered scroll")
	}
	if m.focus != focusDetail {
		t.Fatalf("wheel focus = %v, want detail", m.focus)
	}
	if m.selected != 0 {
		t.Fatalf("detail wheel moved row selection to %d", m.selected)
	}
	updated, _ = m.Update(wheelScrollMsg{seq: m.wheelSeq})
	m = updated.(model)
	if m.detailView.YOffset == 0 {
		t.Fatal("detail pane did not scroll after queued wheel")
	}
}

func TestLeftClickSelectsRowUnderPointer(t *testing.T) {
	items := []Item{
		Row{Kind: "page", Title: "alpha"}.ItemForLayout(LayoutDocument),
		Row{Kind: "page", Title: "bravo"}.ItemForLayout(LayoutDocument),
		Row{Kind: "page", Title: "charlie"}.ItemForLayout(LayoutDocument),
	}
	m := newModel(Options{Title: "archive", Items: items})
	m.width = 100
	m.height = 16
	layout := m.layout()
	updated, _ := m.Update(tea.MouseMsg{
		X:      layout.rows.x + 2,
		Y:      layout.rows.y + 5,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = updated.(model)
	if m.focus != focusRows {
		t.Fatalf("focus = %v, want rows", m.focus)
	}
	if m.selected != 2 {
		t.Fatalf("selected = %d, want row under pointer", m.selected)
	}
	item, ok := m.selectedItem()
	if !ok || item.Title != "charlie" {
		t.Fatalf("selected item = %#v ok=%v", item, ok)
	}
}

func TestRightClickOpensSharedActionMenu(t *testing.T) {
	m := newModel(Options{
		Title: "archive",
		Items: []Item{
			Row{Kind: "message", Title: "alpha"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", Title: "bravo"}.ItemForLayout(LayoutChat),
		},
	})
	m.width = 100
	m.height = 16
	layout := m.layout()
	updated, _ := m.Update(tea.MouseMsg{
		X:      layout.rows.x + 2,
		Y:      layout.rows.y + 4,
		Button: tea.MouseButtonRight,
		Action: tea.MouseActionPress,
	})
	m = updated.(model)
	if !m.menuOpen || m.menuTitle != "Actions" {
		t.Fatalf("menu open=%v title=%q", m.menuOpen, m.menuTitle)
	}
	if m.selected != 1 {
		t.Fatalf("right click selected = %d, want row under pointer", m.selected)
	}
	view := m.View()
	if !strings.Contains(view, "Focus detail pane") || !strings.Contains(view, "Sort focused pane") {
		t.Fatalf("action menu missing expected commands:\n%s", view)
	}
}

func TestSortMenuSortsRowsByStructuredTitle(t *testing.T) {
	m := newModel(Options{
		Title: "archive",
		Items: []Item{
			Row{Kind: "page", Title: "Zulu"}.ItemForLayout(LayoutDocument),
			Row{Kind: "page", Title: "Alpha"}.ItemForLayout(LayoutDocument),
		},
	})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(model)
	if !m.menuOpen || m.menuTitle != "Sort Groups" {
		t.Fatalf("sort menu open=%v title=%q", m.menuOpen, m.menuTitle)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	m = updated.(model)
	if m.sortMode != sortTitle {
		t.Fatalf("sort mode = %v, want title", m.sortMode)
	}
	item, ok := m.selectedItem()
	if !ok || item.Title != "Zulu" {
		t.Fatalf("selected item should stay stable after sorting, got %#v ok=%v", item, ok)
	}
	m.selected = 0
	item, ok = m.selectedItem()
	if !ok || item.Title != "Alpha" {
		t.Fatalf("first sorted item = %#v ok=%v", item, ok)
	}
	view := m.View()
	if !strings.Contains(view, "sort:title") {
		t.Fatalf("header missing sort status:\n%s", view)
	}
}

func TestContextSortMenuSortsMembersWithoutResortingGroups(t *testing.T) {
	m := newModel(Options{
		Title:  "slacrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "z", Scope: "workspace", Container: "general", Author: "Zed", Title: "later", CreatedAt: "2026-05-02T10:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "a", Scope: "workspace", Container: "general", Author: "Amy", Title: "earlier", CreatedAt: "2026-05-02T09:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "x", Scope: "workspace", Container: "random", Author: "Max", Title: "other", CreatedAt: "2026-05-02T11:00:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	m.width = 180
	m.height = 28
	m.focus = focusContext
	beforeGroups := make([]string, 0, len(m.groups))
	for _, group := range m.groups {
		beforeGroups = append(beforeGroups, group.Title)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(model)
	if !m.menuOpen || m.menuTitle != "Sort Members" {
		t.Fatalf("sort menu title = %q open=%v", m.menuTitle, m.menuOpen)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'8'}})
	m = updated.(model)
	if m.sortMode != sortDefault {
		t.Fatalf("group sort changed to %v", m.sortMode)
	}
	if m.memberSortMode != sortAuthor {
		t.Fatalf("member sort = %v, want author", m.memberSortMode)
	}
	afterGroups := make([]string, 0, len(m.groups))
	for _, group := range m.groups {
		afterGroups = append(afterGroups, group.Title)
	}
	if strings.Join(beforeGroups, "\n") != strings.Join(afterGroups, "\n") {
		t.Fatalf("member sort changed group order: before=%v after=%v", beforeGroups, afterGroups)
	}
	members := m.currentGroupMembers()
	if got := m.items[members[0]].Author; got != "Amy" {
		t.Fatalf("first member author = %q, want Amy", got)
	}
}

func TestNewestSortUsesStructuredRowMetadata(t *testing.T) {
	m := newModel(Options{
		Title: "archive",
		Items: []Item{
			Row{Kind: "message", Title: "old", CreatedAt: "2026-05-01T10:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", Title: "new", CreatedAt: "2026-05-02T10:00:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	m.setSortMode(sortNewest)
	m.selected = 0
	item, ok := m.selectedItem()
	if !ok || item.Title != "new" {
		t.Fatalf("newest item = %#v ok=%v", item, ok)
	}
}

func TestHelpMenuRendersUniversalControls(t *testing.T) {
	m := newModel(Options{
		Title: "archive",
		Items: []Item{{Title: "alpha", Tags: []string{"page"}}},
	})
	m.width = 160
	m.height = 34
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(model)
	view := stripANSI(m.View())
	for _, want := range []string{"Help", "Right click or m", "s: sort focused pane", "v: cycle group view", "Mouse click: select pane/row"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help menu missing %q:\n%s", want, view)
		}
	}
}

func TestWideLayoutUsesThreeColumnsByDefault(t *testing.T) {
	m := newModel(Options{
		Title: "archive",
		Items: []Item{{Title: "alpha", Tags: []string{"page"}}},
	})
	m.width = 160
	m.height = 30
	layout := m.layout()
	if layout.mode != string(layoutModeColumns) {
		t.Fatalf("layout mode = %q, want columns", layout.mode)
	}
	if layout.rows.y != layout.context.y || layout.context.y != layout.detail.y {
		t.Fatalf("wide panes should share the same row: %#v", layout)
	}
	if layout.rows.x != 0 || layout.context.x <= layout.rows.x || layout.detail.x <= layout.context.x {
		t.Fatalf("wide panes should progress left-to-right: %#v", layout)
	}
}

func TestChatExplorerGroupsChannelsAndListsMessages(t *testing.T) {
	m := newModel(Options{
		Title:  "discrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", Container: "general", Author: "alice", Title: "first", CreatedAt: "2026-05-01T10:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", Container: "general", Author: "bob", Title: "second", CreatedAt: "2026-05-01T11:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", Container: "random", Author: "alice", Title: "third", CreatedAt: "2026-05-01T12:00:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	m.width = 160
	m.height = 24
	view := m.View()
	if !strings.Contains(view, "Channels") || !strings.Contains(view, "Messages") || !strings.Contains(view, "general") {
		t.Fatalf("chat explorer did not render grouped panes:\n%s", view)
	}
	if len(m.groups) != 2 {
		t.Fatalf("groups = %#v", m.groups)
	}
	for i, group := range m.groups {
		if group.Title == "general" {
			m.selectGroup(i)
			if group.Count != 2 {
				t.Fatalf("general group count = %d", group.Count)
			}
			break
		}
	}
	m.focus = focusContext
	m.moveMember(1)
	item, ok := m.selectedItem()
	if !ok || item.Title != "second" {
		t.Fatalf("selected member = %#v ok=%v", item, ok)
	}
}

func TestChatExplorerCyclesGroupViews(t *testing.T) {
	m := newModel(Options{
		Title:  "discrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "m1", Container: "general", Author: "alice", Title: "first", CreatedAt: "2026-05-01T10:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "m2", Container: "general", Author: "bob", Title: "second", CreatedAt: "2026-05-01T11:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "m3", ParentID: "m1", Container: "general", Author: "alice", Title: "reply", CreatedAt: "2026-05-01T11:30:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	if groupModeLabel(m.layoutPreset, m.groupMode) != "channel" || len(m.groups) != 1 {
		t.Fatalf("default groups = %s %#v", groupModeLabel(m.layoutPreset, m.groupMode), m.groups)
	}
	m.cycleGroupMode()
	if groupModeLabel(m.layoutPreset, m.groupMode) != "person" || len(m.groups) != 2 || m.groupPaneTitle() != "People" {
		t.Fatalf("people groups = %s title=%s %#v", groupModeLabel(m.layoutPreset, m.groupMode), m.groupPaneTitle(), m.groups)
	}
	m.cycleGroupMode()
	if groupModeLabel(m.layoutPreset, m.groupMode) != "thread" || m.groupPaneTitle() != "Threads" {
		t.Fatalf("thread groups = %s title=%s %#v", groupModeLabel(m.layoutPreset, m.groupMode), m.groupPaneTitle(), m.groups)
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "group:thread") || !strings.Contains(view, "Threads") {
		t.Fatalf("view should expose thread group mode:\n%s", view)
	}
}

func TestDocumentExplorerGroupsParentsAndListsPages(t *testing.T) {
	m := newModel(Options{
		Title:  "notcrawl archive",
		Layout: LayoutDocument,
		Items: []Item{
			Row{Kind: "page", ParentID: "folder-a", Title: "Roadmap", UpdatedAt: "2026-05-01T10:00:00Z"}.ItemForLayout(LayoutDocument),
			Row{Kind: "database", ParentID: "folder-a", Title: "Leads", UpdatedAt: "2026-05-01T11:00:00Z"}.ItemForLayout(LayoutDocument),
			Row{Kind: "page", ParentID: "folder-b", Title: "Notes", UpdatedAt: "2026-05-01T12:00:00Z"}.ItemForLayout(LayoutDocument),
		},
	})
	m.width = 160
	m.height = 24
	view := m.View()
	if !strings.Contains(view, "Parents") || !strings.Contains(view, "Pages / Databases") || !strings.Contains(view, "folder-a") {
		t.Fatalf("document explorer did not render parent/member panes:\n%s", view)
	}
	if len(m.groups) != 2 || m.groups[0].Kind != "parent" {
		t.Fatalf("groups = %#v", m.groups)
	}
}

func TestDocumentExplorerCyclesGroupViews(t *testing.T) {
	m := newModel(Options{
		Title:  "notcrawl archive",
		Layout: LayoutDocument,
		Items: []Item{
			Row{Kind: "page", ParentID: "folder-a", Scope: "workspace-a", Container: "Roadmap", Title: "Roadmap", UpdatedAt: "2026-05-01T10:00:00Z"}.ItemForLayout(LayoutDocument),
			Row{Kind: "database", ParentID: "folder-b", Scope: "workspace-a", Container: "Leads", Title: "Leads", UpdatedAt: "2026-05-01T11:00:00Z"}.ItemForLayout(LayoutDocument),
		},
	})
	if groupModeLabel(m.layoutPreset, m.groupMode) != "parent" || len(m.groups) != 2 {
		t.Fatalf("parent groups = %s %#v", groupModeLabel(m.layoutPreset, m.groupMode), m.groups)
	}
	m.cycleGroupMode()
	if groupModeLabel(m.layoutPreset, m.groupMode) != "database" || m.groupPaneTitle() != "Databases" || len(m.groups) != 2 {
		t.Fatalf("database groups = %s title=%s %#v", groupModeLabel(m.layoutPreset, m.groupMode), m.groupPaneTitle(), m.groups)
	}
	m.cycleGroupMode()
	if groupModeLabel(m.layoutPreset, m.groupMode) != "workspace" || m.groupPaneTitle() != "Workspaces" || len(m.groups) != 1 {
		t.Fatalf("workspace groups = %s title=%s %#v", groupModeLabel(m.layoutPreset, m.groupMode), m.groupPaneTitle(), m.groups)
	}
}

func TestLayoutToggleUsesRightStackMode(t *testing.T) {
	m := newModel(Options{
		Title: "archive",
		Items: []Item{{Title: "alpha", Tags: []string{"page"}}},
	})
	m.width = 160
	m.height = 30
	m.toggleLayout()
	layout := m.layout()
	if layout.mode != string(layoutModeRightStack) {
		t.Fatalf("layout mode = %q, want right-stack", layout.mode)
	}
	if layout.context.x != layout.detail.x || layout.detail.y <= layout.context.y {
		t.Fatalf("right stack should place context over detail: %#v", layout)
	}
}

func TestRightClickPlacesFloatingMenu(t *testing.T) {
	m := newModel(Options{
		Title: "archive",
		Items: []Item{{Title: "alpha", Tags: []string{"page"}}},
	})
	m.width = 160
	m.height = 24
	layout := m.layout()
	updated, _ := m.Update(tea.MouseMsg{
		X:      layout.rows.x + 4,
		Y:      layout.rows.y + 3,
		Button: tea.MouseButtonRight,
		Action: tea.MouseActionPress,
	})
	m = updated.(model)
	if !m.menuOpen || !m.menuFloating {
		t.Fatalf("menu open=%v floating=%v", m.menuOpen, m.menuFloating)
	}
	if m.menuRect.w <= 0 || m.menuRect.h <= 0 {
		t.Fatalf("menu rect not placed: %#v", m.menuRect)
	}
	view := m.View()
	if !strings.Contains(view, "Pane") || !strings.Contains(view, "Toggle wide layout") {
		t.Fatalf("floating menu missing expected sections:\n%s", view)
	}
}

func TestClickingRowsHeaderSorts(t *testing.T) {
	m := newModel(Options{
		Title: "archive",
		Items: []Item{
			Row{Kind: "page", Title: "Zulu"}.ItemForLayout(LayoutDocument),
			Row{Kind: "page", Title: "Alpha"}.ItemForLayout(LayoutDocument),
		},
	})
	m.width = 160
	m.height = 24
	layout := m.layout()
	updated, _ := m.Update(tea.MouseMsg{
		X:      layout.rows.x + layout.rows.w - 8,
		Y:      layout.rows.y + 2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = updated.(model)
	if m.sortMode != sortTitle {
		t.Fatalf("sort mode = %v, want title", m.sortMode)
	}
	m.selected = 0
	item, ok := m.selectedItem()
	if !ok || item.Title != "Alpha" {
		t.Fatalf("first sorted item = %#v ok=%v", item, ok)
	}
}

func TestClickingContextHeaderUsesContextPaneColumns(t *testing.T) {
	m := newModel(Options{
		Title:  "slacrawl archive",
		Layout: LayoutChat,
		Items: []Item{
			Row{Kind: "message", ID: "z", Container: "general", Author: "Zed", Title: "later", CreatedAt: "2026-05-02T10:00:00Z"}.ItemForLayout(LayoutChat),
			Row{Kind: "message", ID: "a", Container: "general", Author: "Amy", Title: "earlier", CreatedAt: "2026-05-02T09:00:00Z"}.ItemForLayout(LayoutChat),
		},
	})
	m.width = 300
	m.height = 24
	layout := m.layout()
	contextWidth := paneContentWidth(layout.context.w)
	kindW := minInt(maxInt(5, contextWidth/10), 10)
	whenW := minInt(maxInt(10, contextWidth/6), 16)
	ageW := minInt(maxInt(4, contextWidth/16), 7)
	whereW := minInt(maxInt(10, contextWidth/5), 22)
	authorX := kindW + 1 + whenW + 1 + ageW + 1 + whereW + 1
	updated, _ := m.Update(tea.MouseMsg{
		X:      layout.context.x + 2 + authorX,
		Y:      layout.context.y + 2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = updated.(model)
	if m.memberSortMode != sortAuthor {
		t.Fatalf("member sort mode = %v, want author", m.memberSortMode)
	}
	members := m.currentGroupMembers()
	if got := m.items[members[0]].Author; got != "Amy" {
		t.Fatalf("first author = %q, want Amy", got)
	}
}

func TestRowStyleUsesSubtleSelectedPalette(t *testing.T) {
	selected := rowStyle(80, true, true)
	if fmt.Sprint(selected.GetForeground()) != archiveSelectedFG {
		t.Fatalf("selected foreground = %v, want %s", selected.GetForeground(), archiveSelectedFG)
	}
	if fmt.Sprint(selected.GetBackground()) != archiveSelectedBG {
		t.Fatalf("selected background = %v, want %s", selected.GetBackground(), archiveSelectedBG)
	}
	if fmt.Sprint(selected.GetBackground()) == "#2f3f56" {
		t.Fatal("selected row still uses the old high-contrast blue block")
	}
	blurred := rowStyle(80, true, false)
	if fmt.Sprint(blurred.GetBackground()) != archiveBlurSelectedBG {
		t.Fatalf("blurred selected background = %v, want %s", blurred.GetBackground(), archiveBlurSelectedBG)
	}
}

func TestDocumentLayoutPrioritizesURLDetail(t *testing.T) {
	item := Row{
		Source:    "notion",
		Kind:      "page",
		ID:        "page1",
		Title:     "Launch plan",
		URL:       "https://example.com/launch",
		UpdatedAt: "2026-05-01T12:00:00Z",
	}.ItemForLayout(LayoutDocument)
	if !strings.HasPrefix(item.Detail, "url=https://example.com/launch") {
		t.Fatalf("detail = %q", item.Detail)
	}
	if !strings.Contains(item.Subtitle, "page") || !strings.Contains(item.Subtitle, "2026-05-01") {
		t.Fatalf("subtitle = %q", item.Subtitle)
	}
}

func TestDocumentDetailUsesHeaderLocationPreviewProperties(t *testing.T) {
	item := Row{
		Source:    "notion",
		Kind:      "page",
		ID:        "page1",
		ParentID:  "Launch docs",
		Scope:     "Workspace",
		Container: "Roadmap DB",
		Title:     "Launch plan",
		Text:      "Ship the terminal UI cleanup.",
		URL:       "https://example.com/launch",
		UpdatedAt: "2026-05-01T12:00:00Z",
		Fields:    map[string]string{"space_id": "space1", "parent_table": "collection"},
	}.ItemForLayout(LayoutDocument)
	lines := documentDetailLines(item)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"Launch plan", "Location", "parent=Launch docs", "container=Roadmap DB", "Preview", "Ship the terminal UI cleanup.", "Properties", "updated=2026-05-01 12:00"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("document detail missing %q:\n%s", want, joined)
		}
	}
	if strings.Index(joined, "Preview") > strings.Index(joined, "Properties") {
		t.Fatalf("document preview should come before properties:\n%s", joined)
	}
}

func TestDocumentDetailRendersMarkdownPreviewLikeGitcrawl(t *testing.T) {
	item := Row{
		Source:    "notion",
		Kind:      "page",
		ID:        "page1",
		ParentID:  "Launch docs",
		Scope:     "Workspace",
		Container: "Roadmap DB",
		Title:     "Launch plan",
		Text:      "# Checklist\n- wire panes\n- review [spec](https://example.com/spec)\n> keep it readable",
		UpdatedAt: "2026-05-01T12:00:00Z",
	}.ItemForLayout(LayoutDocument)
	joined := stripANSI(strings.Join(documentDetailLinesForWidth(item, 56, false), "\n"))
	for _, want := range []string{"Launch plan", "Checklist", "- wire panes", "review spec <https://example.com/spec>", "> keep it readable", "Properties"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("document detail missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "# Checklist") {
		t.Fatalf("document detail should render markdown-ish headings:\n%s", joined)
	}
}

func TestDocumentDetailSeparatesProviderAndSource(t *testing.T) {
	item := Row{
		Source: "notion",
		Kind:   "page",
		ID:     "page1",
		Title:  "Launch plan",
		Fields: map[string]string{"source": "desktop", "zeta": "last", "alpha": "first"},
	}.ItemForLayout(LayoutDocument)
	joined := strings.Join(documentDetailLines(item), "\n")
	for _, want := range []string{"provider=notion", "source=desktop"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("document detail missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "source=notion") {
		t.Fatalf("document detail should not duplicate provider as source:\n%s", joined)
	}
	if strings.Index(joined, "alpha=first") > strings.Index(joined, "zeta=last") {
		t.Fatalf("field tail should be stable and sorted:\n%s", joined)
	}
}

func TestModelFilterAndRender(t *testing.T) {
	m := newModel(Options{
		Title: "notcrawl",
		Items: []Item{
			{Title: "Roadmap", Subtitle: "page", Detail: "product plan"},
			{Title: "Invoices", Subtitle: "database", Detail: "finance rows"},
		},
	})
	m.query = "road"
	m.applyFilter()
	if len(m.filtered) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(m.filtered))
	}
	view := m.View()
	if !strings.Contains(view, "Roadmap") {
		t.Fatalf("view missing filtered item:\n%s", view)
	}
	if strings.Contains(view, "Invoices") {
		t.Fatalf("view included filtered-out item:\n%s", view)
	}
}

func TestModelRenderUsesArchivePanesAndSourceFooter(t *testing.T) {
	m := newModel(Options{
		Title:          "notcrawl archive",
		SourceKind:     SourceRemote,
		SourceLocation: "git@example.com:archive/notcrawl.git",
		Items: []Item{{
			Title:    "Roadmap",
			Subtitle: "page  workspace",
			Detail:   "product plan",
			Tags:     []string{"notion", "page"},
		}},
	})
	m.width = 120
	m.height = 24
	view := m.View()
	for _, want := range []string{"Rows", "Context", "Detail", "remote git@example.com:archive/notcrawl.git", "Roadmap", "product plan"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	localBG, _ := footerPalette(SourceLocal)
	remoteBG, _ := footerPalette(SourceRemote)
	if localBG == remoteBG {
		t.Fatal("local and remote footers should use different colors")
	}
}

func TestModelRenderUsesCompleteANSISequencesWhenNarrow(t *testing.T) {
	m := newModel(Options{
		Title: "slacrawl archive",
		Items: []Item{{
			Title:    strings.Repeat("a", 80),
			Subtitle: strings.Repeat("b", 80),
			Detail:   strings.Repeat("c", 80),
			Tags:     []string{"slack", "message", "general"},
		}},
	})
	m.width = 24
	m.height = 12
	view := m.View()
	withoutValidEscapes := stripANSI(view)
	if strings.Contains(withoutValidEscapes, "\x1b") {
		t.Fatalf("view contains broken escape sequence: %q", view)
	}
	if strings.Contains(view, "\x1b[") && !strings.Contains(view, "\x1b[0m") {
		t.Fatalf("styled view did not reset styles: %q", view)
	}
}

func TestWrap(t *testing.T) {
	got := wrap("one two three four", 8)
	if got != "one two\nthree\nfour" {
		t.Fatalf("wrap = %q", got)
	}
}
