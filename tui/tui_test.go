package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
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
