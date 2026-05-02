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
	if strings.Contains(view, "general  vincent") {
		t.Fatalf("rows pane should not append chat metadata:\n%s", view)
	}
	if !strings.Contains(view, "subtitle=general vincent") {
		t.Fatalf("context pane should keep chat metadata:\n%s", view)
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

func TestFocusedDetailPaneScrollsIndependently(t *testing.T) {
	m := newModel(Options{
		Title: "discrawl archive",
		Items: []Item{{
			Title:  "first",
			Detail: strings.Join([]string{"line one", "line two", "line three", "line four", "line five", "line six"}, "\n"),
			Tags:   []string{"message", "discord"},
		}},
	})
	m.width = 80
	m.height = 12
	m.focus = focusDetail
	m.scrollFocused(1)
	if m.selected != 0 {
		t.Fatalf("detail scroll moved row selection to %d", m.selected)
	}
	if m.detailOffset == 0 {
		t.Fatal("detail pane did not scroll")
	}
	view := m.View()
	if !strings.Contains(view, "2-") {
		t.Fatalf("detail pane missing scroll indicator:\n%s", view)
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
			Detail: strings.Join([]string{"line one", "line two", "line three", "line four", "line five", "line six"}, "\n"),
			Tags:   []string{"message", "discord"},
		}},
	})
	m.width = 100
	m.height = 12
	layout := m.layout()
	if m.maxDetailOffset() == 0 {
		t.Fatal("test setup expected scrollable detail")
	}
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
	if m.detailOffset == 0 {
		t.Fatal("detail pane did not scroll after queued wheel")
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
	withoutValidEscapes := regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`).ReplaceAllString(view, "")
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
