package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-isatty"
)

var ErrNotTerminal = errors.New("terminal UI requires an interactive terminal")

const (
	wheelScrollDelay      = 16 * time.Millisecond
	wheelMaxBufferedDelta = 6
	rowsPaneAccent        = "#8fb8d8"
	contextPaneAccent     = "#a8b8a0"
	detailPaneAccent      = "#d3b35f"
	archiveBorderColor    = "#3f4654"
	archiveHeaderBG       = "#151a24"
	archiveHeaderFG       = "#dbe3ee"
	archiveTextFG         = "#c9d2de"
	archiveMutedFG        = "#8d98a8"
	archiveSubtleAccentFG = "#8fb8d8"
	archiveSelectedFG     = "#f0d070"
	archiveSelectedBG     = "#242215"
	archiveBlurSelectedFG = "#c8bc86"
	archiveBlurSelectedBG = "#1b1b15"
	archiveRemoteFooterBG = "#d3b35f"
	archiveLocalFooterBG  = "#8fb8d8"
	archiveFooterFG       = "#05070d"
)

type wheelScrollMsg struct {
	seq int
}

type Item struct {
	Title     string            `json:"title"`
	Subtitle  string            `json:"subtitle,omitempty"`
	Detail    string            `json:"detail,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Depth     int               `json:"depth,omitempty"`
	Source    string            `json:"source,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	ID        string            `json:"id,omitempty"`
	ParentID  string            `json:"parent_id,omitempty"`
	Scope     string            `json:"scope,omitempty"`
	Container string            `json:"container,omitempty"`
	Author    string            `json:"author,omitempty"`
	URL       string            `json:"url,omitempty"`
	CreatedAt string            `json:"created_at,omitempty"`
	UpdatedAt string            `json:"updated_at,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type LayoutPreset string

const (
	LayoutAuto     LayoutPreset = ""
	LayoutList     LayoutPreset = "list"
	LayoutChat     LayoutPreset = "chat"
	LayoutDocument LayoutPreset = "document"
)

const (
	SourceLocal  = "local"
	SourceRemote = "remote"
)

type Row struct {
	Source    string            `json:"source,omitempty"`
	Kind      string            `json:"kind"`
	ID        string            `json:"id,omitempty"`
	ParentID  string            `json:"parent_id,omitempty"`
	Depth     int               `json:"depth,omitempty"`
	Scope     string            `json:"scope,omitempty"`
	Container string            `json:"container,omitempty"`
	Author    string            `json:"author,omitempty"`
	Title     string            `json:"title"`
	Text      string            `json:"text,omitempty"`
	URL       string            `json:"url,omitempty"`
	CreatedAt string            `json:"created_at,omitempty"`
	UpdatedAt string            `json:"updated_at,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type Options struct {
	Title          string
	EmptyMessage   string
	Items          []Item
	SourceKind     string
	SourceLocation string
	Stdin          io.Reader
	Stdout         io.Writer
}

type BrowseOptions struct {
	AppName        string
	Title          string
	EmptyMessage   string
	Rows           []Row
	JSON           bool
	Layout         LayoutPreset
	SourceKind     string
	SourceLocation string
	Stdin          io.Reader
	Stdout         io.Writer
}

func Browse(ctx context.Context, opts BrowseOptions) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.JSON {
		if opts.Rows == nil {
			opts.Rows = []Row{}
		}
		enc := json.NewEncoder(opts.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(opts.Rows)
	}
	items := make([]Item, 0, len(opts.Rows))
	layout := opts.Layout
	if layout == LayoutAuto {
		layout = inferLayout(opts.Rows)
	}
	for _, row := range opts.Rows {
		items = append(items, row.ItemForLayout(layout))
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = strings.TrimSpace(opts.AppName)
		if title != "" {
			title += " archive"
		}
	}
	if title == "" {
		title = "archive"
	}
	empty := strings.TrimSpace(opts.EmptyMessage)
	if empty == "" && strings.TrimSpace(opts.AppName) != "" {
		empty = opts.AppName + " has no local archive rows yet"
	}
	err := Run(ctx, Options{
		Title:          title,
		EmptyMessage:   empty,
		Items:          items,
		SourceKind:     opts.SourceKind,
		SourceLocation: opts.SourceLocation,
		Stdin:          opts.Stdin,
		Stdout:         opts.Stdout,
	})
	if err != nil && errors.Is(err, ErrNotTerminal) {
		app := strings.TrimSpace(opts.AppName)
		if app == "" {
			return fmt.Errorf("%w; run tui from a TTY or pass --json", err)
		}
		return fmt.Errorf("%w; run %s tui from a TTY or pass --json", err, app)
	}
	return err
}

func (r Row) Item() Item {
	return r.ItemForLayout(LayoutAuto)
}

func (r Row) ItemForLayout(layout LayoutPreset) Item {
	if layout == LayoutAuto {
		layout = inferLayout([]Row{r})
	}
	rawTitle := firstNonEmpty(r.Title, r.Text, r.ID, "(untitled)")
	title := compactTitle(rawTitle)
	detail := r.detailForLayout(layout)
	if strings.TrimSpace(detail) == "" && title != strings.TrimSpace(rawTitle) {
		detail = strings.TrimSpace(rawTitle)
	}
	tags := append([]string(nil), r.Tags...)
	if r.Kind != "" {
		tags = append([]string{r.Kind}, tags...)
	}
	if r.Source != "" {
		tags = append([]string{r.Source}, tags...)
	}
	depth := r.Depth
	if depth == 0 && layout == LayoutChat && strings.TrimSpace(r.ParentID) != "" {
		depth = 1
	}
	return Item{
		Title:     title,
		Subtitle:  r.subtitleForLayout(layout),
		Detail:    detail,
		Tags:      tags,
		Depth:     depth,
		Source:    strings.TrimSpace(r.Source),
		Kind:      strings.TrimSpace(r.Kind),
		ID:        strings.TrimSpace(r.ID),
		ParentID:  strings.TrimSpace(r.ParentID),
		Scope:     strings.TrimSpace(r.Scope),
		Container: strings.TrimSpace(r.Container),
		Author:    strings.TrimSpace(r.Author),
		URL:       strings.TrimSpace(r.URL),
		CreatedAt: strings.TrimSpace(r.CreatedAt),
		UpdatedAt: strings.TrimSpace(r.UpdatedAt),
		Fields:    copyStringMap(r.Fields),
	}
}

func Run(ctx context.Context, opts Options) error {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if len(opts.Items) == 0 {
		msg := opts.EmptyMessage
		if msg == "" {
			msg = "no rows"
		}
		_, err := fmt.Fprintln(opts.Stdout, msg)
		return err
	}
	input, ok := opts.Stdin.(*os.File)
	if !ok || !isatty.IsTerminal(input.Fd()) {
		return ErrNotTerminal
	}
	output, ok := opts.Stdout.(*os.File)
	if !ok || !isatty.IsTerminal(output.Fd()) {
		return ErrNotTerminal
	}
	program := tea.NewProgram(
		newModel(opts),
		tea.WithContext(ctx),
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := program.Run()
	return err
}

func inferLayout(rows []Row) LayoutPreset {
	for _, row := range rows {
		switch strings.ToLower(strings.TrimSpace(row.Kind)) {
		case "message", "thread", "reply":
			return LayoutChat
		case "page", "database", "block", "collection":
			return LayoutDocument
		}
	}
	return LayoutList
}

func (r Row) subtitleForLayout(layout LayoutPreset) string {
	if layout == LayoutChat {
		parts := []string{r.Container, r.Author, r.CreatedAt, r.UpdatedAt}
		return joinNonEmpty(parts, "  ")
	}
	if layout == LayoutDocument {
		parts := []string{r.Kind, r.Scope, r.Container, r.UpdatedAt, r.CreatedAt}
		return joinNonEmpty(parts, "  ")
	}
	parts := []string{r.Scope, r.Container, r.Author, r.CreatedAt, r.UpdatedAt}
	return joinNonEmpty(parts, "  ")
}

func joinNonEmpty(parts []string, sep string) string {
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, sep)
}

func (r Row) detailForLayout(layout LayoutPreset) string {
	var lines []string
	if text := strings.TrimSpace(r.Text); text != "" && text != strings.TrimSpace(r.Title) {
		lines = append(lines, text)
	}
	if layout == LayoutDocument && strings.TrimSpace(r.URL) != "" {
		lines = append(lines, fieldLine("url", r.URL))
	}
	for _, line := range []string{
		fieldLine("id", r.ID),
		fieldLine("parent", r.ParentID),
		fieldLine("scope", r.Scope),
		fieldLine("container", r.Container),
		fieldLine("author", r.Author),
	} {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if layout != LayoutDocument {
		if line := fieldLine("url", r.URL); line != "" {
			lines = append(lines, line)
		}
	}
	if len(r.Fields) > 0 {
		keys := make([]string, 0, len(r.Fields))
		for key := range r.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if line := fieldLine(key, r.Fields[key]); line != "" {
				lines = append(lines, line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func fieldLine(key, value string) string {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return ""
	}
	return key + "=" + value
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func compactTitle(value string) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if value == "" {
		return ""
	}
	for _, sep := range []string{". ", "\n"} {
		if idx := strings.Index(value, sep); idx > 18 {
			value = strings.TrimSpace(value[:idx+1])
			break
		}
	}
	return truncateCells(value, 140)
}

type model struct {
	title          string
	items          []Item
	filtered       []int
	selected       int
	offset         int
	width          int
	height         int
	query          string
	filterMode     bool
	focus          paneFocus
	contextOffset  int
	detailOffset   int
	wheelPending   bool
	wheelFocus     paneFocus
	wheelDelta     int
	wheelSeq       int
	sourceKind     string
	sourceLocation string
	sortMode       sortMode
	menuOpen       bool
	menuTitle      string
	menuItems      []menuItem
	menuIndex      int
}

type sortMode int

const (
	sortDefault sortMode = iota
	sortNewest
	sortOldest
	sortTitle
	sortKind
	sortScope
	sortContainer
	sortAuthor
)

type menuAction int

const (
	actionClose menuAction = iota
	actionFocusRows
	actionFocusContext
	actionFocusDetail
	actionSortMenu
	actionHelpMenu
	actionClearFilter
	actionQuit
	actionSortDefault
	actionSortNewest
	actionSortOldest
	actionSortTitle
	actionSortKind
	actionSortScope
	actionSortContainer
	actionSortAuthor
)

type menuItem struct {
	label  string
	action menuAction
}

func newModel(opts Options) model {
	m := model{
		title:          strings.TrimSpace(opts.Title),
		items:          append([]Item(nil), opts.Items...),
		width:          100,
		height:         30,
		focus:          focusRows,
		sourceKind:     normalizeSourceKind(opts.SourceKind),
		sourceLocation: strings.TrimSpace(opts.SourceLocation),
	}
	if m.title == "" {
		m.title = "archive"
	}
	m.applyFilter()
	return m
}

type paneFocus int

const (
	focusRows paneFocus = iota
	focusContext
	focusDetail
)

type rect struct {
	x int
	y int
	w int
	h int
}

type archiveLayout struct {
	rows    rect
	context rect
	detail  rect
	stacked bool
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = maxInt(typed.Width, 40)
		m.height = maxInt(typed.Height, 12)
		m.ensureVisible()
	case wheelScrollMsg:
		if typed.seq != m.wheelSeq {
			return m, nil
		}
		m.applyQueuedWheelScroll()
		return m, nil
	case tea.MouseMsg:
		switch {
		case typed.Type == tea.MouseWheelUp || typed.Button == tea.MouseButtonWheelUp:
			return m, m.queueWheelScroll(m.paneAt(typed.X, typed.Y), -3)
		case typed.Type == tea.MouseWheelDown || typed.Button == tea.MouseButtonWheelDown:
			return m, m.queueWheelScroll(m.paneAt(typed.X, typed.Y), 3)
		case typed.Button == tea.MouseButtonLeft && typed.Action == tea.MouseActionPress:
			m.handleLeftClick(typed.X, typed.Y)
		case typed.Button == tea.MouseButtonRight && typed.Action == tea.MouseActionPress:
			m.handleRightClick(typed.X, typed.Y)
		}
	case tea.KeyMsg:
		m.cancelQueuedWheelScroll()
		if m.menuOpen {
			if cmd := m.updateMenuKey(typed); cmd != nil {
				return m, cmd
			}
			return m, nil
		}
		if m.filterMode {
			switch typed.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "enter", "esc":
				m.filterMode = false
			case "backspace":
				if len(m.query) > 0 {
					m.query = m.query[:len(m.query)-1]
					m.applyFilter()
				}
			default:
				if len(typed.Runes) > 0 {
					m.query += string(typed.Runes)
					m.applyFilter()
				}
			}
			return m, nil
		}
		switch typed.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab", "right":
			m.focus = nextFocus(m.focus, 1)
		case "shift+tab", "left":
			m.focus = nextFocus(m.focus, -1)
		case "up", "k":
			if m.focus == focusRows {
				m.move(-1)
			} else {
				m.scrollFocused(-1)
			}
		case "down", "j":
			if m.focus == focusRows {
				m.move(1)
			} else {
				m.scrollFocused(1)
			}
		case "pgup", "ctrl+b":
			if m.focus == focusRows {
				m.move(-m.pageSize())
			} else {
				m.scrollFocused(-m.focusedPageSize())
			}
		case "pgdown", "ctrl+f":
			if m.focus == focusRows {
				m.move(m.pageSize())
			} else {
				m.scrollFocused(m.focusedPageSize())
			}
		case "home", "g":
			if m.focus == focusRows {
				m.selected = 0
				m.ensureVisible()
			}
		case "end", "G":
			if m.focus == focusRows {
				m.selected = len(m.filtered) - 1
				m.ensureVisible()
			}
		case "/", "f":
			m.closeMenu()
			m.filterMode = true
		case "s":
			m.openSortMenu()
		case "m":
			m.openActionMenu()
		case "?":
			m.openHelpMenu()
		case "esc":
			if m.query != "" {
				m.query = ""
				m.applyFilter()
			}
		case "enter", " ":
			m.focus = focusDetail
		}
	}
	return m, nil
}

func (m *model) handleLeftClick(x, y int) {
	layout := m.layout()
	if m.menuOpen && layout.context.contains(x, y) {
		if row, ok := m.menuRowAt(layout.context, y); ok {
			m.menuIndex = row
			_ = m.runMenuAction(m.menuItems[m.menuIndex].action)
			return
		}
	}
	m.closeMenu()
	focus := m.paneAt(x, y)
	m.focus = focus
	if focus == focusRows {
		m.selectRowAt(layout.rows, y)
	}
}

func (m *model) handleRightClick(x, y int) {
	focus := m.paneAt(x, y)
	m.focus = focus
	if focus == focusRows {
		m.selectRowAt(m.layout().rows, y)
	}
	m.openActionMenu()
}

func (m *model) selectRowAt(rect rect, y int) {
	row := y - rect.y - 2
	if row < 0 || row >= paneContentHeight(rect.h) {
		return
	}
	selected := m.offset + row
	if selected < 0 || selected >= len(m.filtered) {
		return
	}
	m.selected = selected
	m.contextOffset = 0
	m.detailOffset = 0
	m.ensureVisible()
}

func (m model) menuRowAt(rect rect, y int) (int, bool) {
	row := y - rect.y - 2
	if row < 0 || row >= len(m.menuItems) {
		return 0, false
	}
	return row, true
}

func (m *model) updateMenuKey(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc", "q":
		m.closeMenu()
	case "up", "k":
		m.menuIndex = clampInt(m.menuIndex-1, 0, maxInt(0, len(m.menuItems)-1))
	case "down", "j":
		m.menuIndex = clampInt(m.menuIndex+1, 0, maxInt(0, len(m.menuItems)-1))
	case "enter", " ":
		if len(m.menuItems) > 0 {
			return m.runMenuAction(m.menuItems[m.menuIndex].action)
		}
	case "s":
		m.openSortMenu()
	case "?":
		m.openHelpMenu()
	}
	return nil
}

func (m *model) openActionMenu() {
	items := []menuItem{
		{label: "Focus rows pane", action: actionFocusRows},
		{label: "Focus context pane", action: actionFocusContext},
		{label: "Focus detail pane", action: actionFocusDetail},
		{label: "Sort rows", action: actionSortMenu},
	}
	if m.query != "" {
		items = append(items, menuItem{label: "Clear filter", action: actionClearFilter})
	}
	items = append(items,
		menuItem{label: "Help", action: actionHelpMenu},
		menuItem{label: "Close menu", action: actionClose},
	)
	m.openMenu("Actions", items)
}

func (m *model) openSortMenu() {
	m.openMenu("Sort", []menuItem{
		{label: markActiveSort("Default", m.sortMode == sortDefault), action: actionSortDefault},
		{label: markActiveSort("Newest", m.sortMode == sortNewest), action: actionSortNewest},
		{label: markActiveSort("Oldest", m.sortMode == sortOldest), action: actionSortOldest},
		{label: markActiveSort("Title", m.sortMode == sortTitle), action: actionSortTitle},
		{label: markActiveSort("Kind", m.sortMode == sortKind), action: actionSortKind},
		{label: markActiveSort("Scope", m.sortMode == sortScope), action: actionSortScope},
		{label: markActiveSort("Container", m.sortMode == sortContainer), action: actionSortContainer},
		{label: markActiveSort("Author", m.sortMode == sortAuthor), action: actionSortAuthor},
	})
}

func (m *model) openHelpMenu() {
	m.openMenu("Help", []menuItem{
		{label: "Tab/arrow: select pane", action: actionClose},
		{label: "Mouse click: select pane/row", action: actionClose},
		{label: "Right click or m: actions", action: actionClose},
		{label: "s: sort rows", action: actionClose},
		{label: "/: filter rows", action: actionClose},
		{label: "j/k or wheel: scroll focused pane", action: actionClose},
		{label: "enter: detail pane", action: actionClose},
		{label: "q: quit", action: actionQuit},
	})
}

func (m *model) openMenu(title string, items []menuItem) {
	m.menuOpen = true
	m.menuTitle = title
	m.menuItems = append([]menuItem(nil), items...)
	m.menuIndex = clampInt(m.menuIndex, 0, maxInt(0, len(m.menuItems)-1))
	m.filterMode = false
}

func (m *model) closeMenu() {
	m.menuOpen = false
	m.menuTitle = ""
	m.menuItems = nil
	m.menuIndex = 0
}

func (m *model) runMenuAction(action menuAction) tea.Cmd {
	switch action {
	case actionClose:
		m.closeMenu()
	case actionFocusRows:
		m.focus = focusRows
		m.closeMenu()
	case actionFocusContext:
		m.focus = focusContext
		m.closeMenu()
	case actionFocusDetail:
		m.focus = focusDetail
		m.closeMenu()
	case actionSortMenu:
		m.openSortMenu()
	case actionHelpMenu:
		m.openHelpMenu()
	case actionClearFilter:
		m.query = ""
		m.applyFilter()
		m.closeMenu()
	case actionSortDefault:
		m.setSortMode(sortDefault)
	case actionSortNewest:
		m.setSortMode(sortNewest)
	case actionSortOldest:
		m.setSortMode(sortOldest)
	case actionSortTitle:
		m.setSortMode(sortTitle)
	case actionSortKind:
		m.setSortMode(sortKind)
	case actionSortScope:
		m.setSortMode(sortScope)
	case actionSortContainer:
		m.setSortMode(sortContainer)
	case actionSortAuthor:
		m.setSortMode(sortAuthor)
	case actionQuit:
		return tea.Quit
	}
	return nil
}

func (m model) View() string {
	width := maxInt(m.width, 40)
	height := maxInt(m.height, 12)
	layout := m.layout()
	header := m.renderHeader(width)
	rows := m.renderRowsPane(layout.rows)
	context := m.renderContextPane(layout.context)
	detail := m.renderDetailPane(layout.detail)
	footer := m.renderFooter(width)
	var body string
	if layout.stacked {
		body = lipgloss.JoinVertical(lipgloss.Left, rows, context, detail)
	} else {
		right := lipgloss.JoinVertical(lipgloss.Left, context, detail)
		body = lipgloss.JoinHorizontal(lipgloss.Top, rows, right)
	}
	view := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return fitBlock(view, width, height)
}

func (m model) renderHeader(width int) string {
	status := fmt.Sprintf("%d/%d rows", len(m.filtered), len(m.items))
	if m.query != "" {
		status += " filtered by " + strconvQuote(m.query)
	}
	if m.sortMode != sortDefault {
		status += " sort " + m.sortMode.Label()
	}
	line := m.title + "  " + status
	if m.filterMode {
		line += "  filter> " + m.query
	} else if m.menuOpen {
		line += "  menu> " + m.menuTitle
	}
	return titleStyle(width).Render(padCells(" "+truncateCells(line, maxInt(1, width-2)), width))
}

func (m model) renderRowsPane(rect rect) string {
	var lines []string
	if m.filterMode {
		lines = append(lines, accentStyle().Render("filter> ")+m.query)
	}
	if len(m.filtered) == 0 {
		lines = append(lines, mutedStyle(rect.w).Render("no rows match"))
	} else {
		for _, index := range m.visibleRows() {
			item := m.items[m.filtered[index]]
			selected := index == m.selected
			prefix := "  "
			if selected {
				prefix = "> "
			}
			line := prefix + rowListLine(item, paneContentWidth(rect.w)-lipgloss.Width(prefix))
			lines = append(lines, rowStyle(paneContentWidth(rect.w), selected, m.focus == focusRows).Render(line))
		}
	}
	return pane("Rows", m.positionLabel(), lines, rect, m.focus == focusRows, rowsPaneAccent)
}

func (m model) renderContextPane(rect rect) string {
	if m.menuOpen {
		return pane(m.menuTitle, "enter choose  esc close", m.menuLines(paneContentWidth(rect.w)), rect, true, contextPaneAccent)
	}
	item, ok := m.selectedItem()
	if !ok {
		return pane("Context", "", []string{"No row selected."}, rect, m.focus == focusContext, contextPaneAccent)
	}
	lines := contextLines(item, paneContentWidth(rect.w))
	return paneScrolled("Context", paneFocusLabel(m.focus == focusContext), lines, rect, m.focus == focusContext, contextPaneAccent, m.contextOffset)
}

func (m model) renderDetailPane(rect rect) string {
	item, ok := m.selectedItem()
	if !ok {
		return pane("Detail", "", []string{"No row selected."}, rect, m.focus == focusDetail, detailPaneAccent)
	}
	lines := detailLines(item)
	return paneScrolled("Detail", paneFocusLabel(m.focus == focusDetail), lines, rect, m.focus == focusDetail, detailPaneAccent, m.detailOffset)
}

func (m model) renderFooter(width int) string {
	line := "Ready"
	if m.filterMode {
		line = "Filtering"
	} else if m.menuOpen {
		line = "Menu"
	}
	if location := m.footerLocation(); location != "" {
		line += "  " + location
	}
	controls := "Tab panes  click select  right-click/m menu  s sort  / filter  enter details  q quit"
	bg, fg := footerPalette(m.sourceKind)
	statusLine := padCells(" "+truncateCells(line, maxInt(1, width-2)), width)
	controlsLine := padCells(" "+truncateCells(controls, maxInt(1, width-2)), width)
	return lipgloss.NewStyle().Width(width).Height(2).Background(bg).Foreground(fg).Render(statusLine + "\n" + controlsLine)
}

func (m model) menuLines(width int) []string {
	if len(m.menuItems) == 0 {
		return []string{"No actions."}
	}
	lines := make([]string, 0, len(m.menuItems))
	for i, item := range m.menuItems {
		prefix := "  "
		if i == m.menuIndex {
			prefix = "> "
		}
		lines = append(lines, truncateCells(prefix+item.label, width))
	}
	return lines
}

func (m model) footerLocation() string {
	location := strings.TrimSpace(m.sourceLocation)
	if location == "" {
		return m.sourceKind
	}
	return m.sourceKind + " " + location
}

func (m *model) move(delta int) {
	if len(m.filtered) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	m.selected = clampInt(m.selected+delta, 0, len(m.filtered)-1)
	m.contextOffset = 0
	m.detailOffset = 0
	m.ensureVisible()
}

func (m *model) scrollFocused(delta int) {
	switch m.focus {
	case focusContext:
		m.contextOffset = clampInt(m.contextOffset+delta, 0, m.maxContextOffset())
	case focusDetail:
		m.detailOffset = clampInt(m.detailOffset+delta, 0, m.maxDetailOffset())
	default:
		m.move(delta)
	}
}

func (m *model) queueWheelScroll(focus paneFocus, delta int) tea.Cmd {
	if delta == 0 {
		return nil
	}
	if m.wheelPending && m.wheelFocus != focus {
		m.cancelQueuedWheelScroll()
	}
	m.focus = focus
	m.wheelFocus = focus
	m.wheelDelta = clampInt(m.wheelDelta+delta, -wheelMaxBufferedDelta, wheelMaxBufferedDelta)
	if m.wheelPending {
		return nil
	}
	m.wheelPending = true
	m.wheelSeq++
	seq := m.wheelSeq
	return tea.Tick(wheelScrollDelay, func(time.Time) tea.Msg {
		return wheelScrollMsg{seq: seq}
	})
}

func (m *model) cancelQueuedWheelScroll() {
	if !m.wheelPending && m.wheelDelta == 0 {
		return
	}
	m.wheelPending = false
	m.wheelDelta = 0
	m.wheelSeq++
}

func (m *model) applyQueuedWheelScroll() {
	delta := m.wheelDelta
	focus := m.wheelFocus
	m.wheelPending = false
	m.wheelDelta = 0
	if delta == 0 {
		return
	}
	m.focus = focus
	m.scrollFocused(delta)
}

func (m model) focusedPageSize() int {
	layout := m.layout()
	switch m.focus {
	case focusContext:
		return maxInt(1, paneContentHeight(layout.context.h))
	case focusDetail:
		return maxInt(1, paneContentHeight(layout.detail.h))
	default:
		return m.pageSize()
	}
}

func (m model) maxContextOffset() int {
	item, ok := m.selectedItem()
	if !ok {
		return 0
	}
	layout := m.layout()
	return maxPaneScroll(contextLines(item, paneContentWidth(layout.context.w)), layout.context)
}

func (m model) maxDetailOffset() int {
	item, ok := m.selectedItem()
	if !ok {
		return 0
	}
	layout := m.layout()
	return maxPaneScroll(detailLines(item), layout.detail)
}

func (m *model) applyFilter() {
	current := m.currentItemIndex()
	query := strings.ToLower(strings.TrimSpace(m.query))
	m.filtered = m.filtered[:0]
	for i, item := range m.items {
		if query == "" || strings.Contains(strings.ToLower(item.searchText()), query) {
			m.filtered = append(m.filtered, i)
		}
	}
	m.sortFiltered()
	if len(m.filtered) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	if current >= 0 {
		for i, index := range m.filtered {
			if index == current {
				m.selected = i
				break
			}
		}
	}
	m.selected = clampInt(m.selected, 0, len(m.filtered)-1)
	m.ensureVisible()
}

func (m *model) setSortMode(mode sortMode) {
	m.sortMode = mode
	m.applyFilter()
	m.closeMenu()
}

func (m model) currentItemIndex() int {
	if len(m.filtered) == 0 || m.selected < 0 || m.selected >= len(m.filtered) {
		return -1
	}
	return m.filtered[m.selected]
}

func (m *model) sortFiltered() {
	if m.sortMode == sortDefault {
		return
	}
	sort.SliceStable(m.filtered, func(i, j int) bool {
		left := m.items[m.filtered[i]]
		right := m.items[m.filtered[j]]
		if less, ok := compareItems(left, right, m.sortMode); ok {
			return less
		}
		return m.filtered[i] < m.filtered[j]
	})
}

func (m *model) ensureVisible() {
	page := m.pageSize()
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+page {
		m.offset = m.selected - page + 1
	}
	m.offset = clampInt(m.offset, 0, maxInt(len(m.filtered)-1, 0))
}

func (m model) pageSize() int {
	return maxInt(1, paneContentHeight(m.layout().rows.h))
}

func (m model) visibleRows() []int {
	end := minInt(len(m.filtered), m.offset+m.pageSize())
	out := make([]int, 0, end-m.offset)
	for i := m.offset; i < end; i++ {
		out = append(out, i)
	}
	return out
}

func (m model) layout() archiveLayout {
	width := maxInt(m.width, 40)
	height := maxInt(m.height, 12)
	bodyH := maxInt(6, height-3)
	if width >= 96 {
		rowsW := maxInt(38, width*44/100)
		rightW := width - rowsW
		contextH := minInt(maxInt(7, bodyH/3), maxInt(5, bodyH-4))
		return archiveLayout{
			rows:    rect{x: 0, y: 1, w: rowsW, h: bodyH},
			context: rect{x: rowsW, y: 1, w: rightW, h: contextH},
			detail:  rect{x: rowsW, y: 1 + contextH, w: rightW, h: bodyH - contextH},
		}
	}
	rowsH := maxInt(5, bodyH*42/100)
	contextH := minInt(maxInt(4, bodyH*24/100), maxInt(3, bodyH-rowsH-3))
	return archiveLayout{
		rows:    rect{x: 0, y: 1, w: width, h: rowsH},
		context: rect{x: 0, y: 1 + rowsH, w: width, h: contextH},
		detail:  rect{x: 0, y: 1 + rowsH + contextH, w: width, h: bodyH - rowsH - contextH},
		stacked: true,
	}
}

func (m model) paneAt(x, y int) paneFocus {
	layout := m.layout()
	switch {
	case layout.rows.contains(x, y):
		return focusRows
	case layout.context.contains(x, y):
		return focusContext
	case layout.detail.contains(x, y):
		return focusDetail
	default:
		return m.focus
	}
}

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

func (m model) selectedItem() (Item, bool) {
	if len(m.filtered) == 0 || m.selected < 0 || m.selected >= len(m.filtered) {
		return Item{}, false
	}
	index := m.filtered[m.selected]
	if index < 0 || index >= len(m.items) {
		return Item{}, false
	}
	return m.items[index], true
}

func (s sortMode) Label() string {
	switch s {
	case sortNewest:
		return "newest"
	case sortOldest:
		return "oldest"
	case sortTitle:
		return "title"
	case sortKind:
		return "kind"
	case sortScope:
		return "scope"
	case sortContainer:
		return "container"
	case sortAuthor:
		return "author"
	default:
		return "default"
	}
}

func markActiveSort(label string, active bool) string {
	if active {
		return label + " *"
	}
	return label
}

func compareItems(left, right Item, mode sortMode) (bool, bool) {
	switch mode {
	case sortNewest:
		return compareItemTime(left, right, true)
	case sortOldest:
		return compareItemTime(left, right, false)
	case sortTitle:
		return compareStrings(left.Title, right.Title)
	case sortKind:
		return compareStrings(itemKind(left), itemKind(right))
	case sortScope:
		return compareStrings(itemScope(left), itemScope(right))
	case sortContainer:
		return compareStrings(itemContainer(left), itemContainer(right))
	case sortAuthor:
		return compareStrings(itemAuthor(left), itemAuthor(right))
	default:
		return false, false
	}
}

func compareItemTime(left, right Item, newest bool) (bool, bool) {
	leftTime, leftOK := itemSortTime(left)
	rightTime, rightOK := itemSortTime(right)
	if leftOK != rightOK {
		return leftOK, true
	}
	if !leftOK {
		return false, false
	}
	if leftTime.Equal(rightTime) {
		return false, false
	}
	if newest {
		return leftTime.After(rightTime), true
	}
	return leftTime.Before(rightTime), true
}

func compareStrings(left, right string) (bool, bool) {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	if left == right {
		return false, false
	}
	if left == "" {
		return false, true
	}
	if right == "" {
		return true, true
	}
	return left < right, true
}

func (m model) positionLabel() string {
	if len(m.filtered) == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d", m.selected+1, len(m.filtered))
}

func nextFocus(focus paneFocus, delta int) paneFocus {
	next := int(focus) + delta
	if next < int(focusRows) {
		return focusDetail
	}
	if next > int(focusDetail) {
		return focusRows
	}
	return paneFocus(next)
}

func paneFocusLabel(focused bool) string {
	if focused {
		return "focused"
	}
	return ""
}

func pane(title, subtitle string, lines []string, rect rect, focused bool, accent string) string {
	return paneScrolled(title, subtitle, lines, rect, focused, accent, 0)
}

func paneScrolled(title, subtitle string, lines []string, rect rect, focused bool, accent string, scrollOffset int) string {
	width := maxInt(rect.w, 12)
	height := maxInt(rect.h, 3)
	contentW := paneContentWidth(width)
	contentH := paneContentHeight(height)
	body := flattenedPaneLines(lines, contentW)
	if len(body) == 0 {
		body = append(body, "")
	}
	maxOffset := maxInt(0, len(body)-contentH)
	scrollOffset = clampInt(scrollOffset, 0, maxOffset)
	if maxOffset > 0 {
		visibleEnd := minInt(len(body), scrollOffset+contentH)
		scrollLabel := fmt.Sprintf("%d-%d/%d", scrollOffset+1, visibleEnd, len(body))
		if strings.TrimSpace(subtitle) == "" {
			subtitle = scrollLabel
		} else {
			subtitle += "  " + scrollLabel
		}
	}
	borderColor := archiveBorderColor
	if focused {
		borderColor = accent
	}
	titleLine := title
	if strings.TrimSpace(subtitle) != "" {
		titleLine += "  " + subtitle
	}
	top := "+" + strings.Repeat("-", maxInt(0, width-2)) + "+"
	border := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(archiveHeaderFG)).Bold(focused)
	header := border.Render("|") +
		headerStyle.Render(padCells(" "+truncateCells(titleLine, maxInt(1, contentW-1)), contentW)) +
		border.Render("|")
	body = append([]string(nil), body[scrollOffset:minInt(len(body), scrollOffset+contentH)]...)
	for len(body) < contentH {
		body = append(body, "")
	}
	out := []string{border.Render(top), header}
	for _, line := range body[:contentH] {
		out = append(out, border.Render("|")+padCells(truncateCells(line, contentW), contentW)+border.Render("|"))
	}
	out = append(out, border.Render(top))
	return strings.Join(out, "\n")
}

func flattenedPaneLines(lines []string, width int) []string {
	var body []string
	for _, line := range lines {
		body = append(body, wrapLines(line, width)...)
	}
	return body
}

func maxPaneScroll(lines []string, rect rect) int {
	body := flattenedPaneLines(lines, paneContentWidth(rect.w))
	if len(body) == 0 {
		return 0
	}
	return maxInt(0, len(body)-paneContentHeight(rect.h))
}

func paneContentWidth(width int) int {
	return maxInt(1, width-2)
}

func paneContentHeight(height int) int {
	return maxInt(1, height-3)
}

func compactNonEmpty(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func (item Item) searchText() string {
	parts := []string{
		item.Title,
		item.Subtitle,
		item.Detail,
		item.Source,
		item.Kind,
		item.ID,
		item.ParentID,
		item.Scope,
		item.Container,
		item.Author,
		item.URL,
		item.CreatedAt,
		item.UpdatedAt,
		strings.Join(item.Tags, " "),
	}
	if len(item.Fields) > 0 {
		keys := make([]string, 0, len(item.Fields))
		for key := range item.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, key, item.Fields[key])
		}
	}
	return strings.Join(parts, " ")
}

func contextLines(item Item, width int) []string {
	lines := []string{
		fieldLine("title", truncateCells(item.Title, maxInt(1, width-6))),
		fieldLine("subtitle", item.Subtitle),
	}
	for _, line := range []string{
		fieldLine("source", item.Source),
		fieldLine("kind", item.Kind),
		fieldLine("id", item.ID),
		fieldLine("scope", item.Scope),
		fieldLine("container", item.Container),
		fieldLine("author", item.Author),
	} {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(item.Tags) > 0 {
		lines = append(lines, "tags="+strings.Join(item.Tags, " "))
	}
	return compactNonEmpty(lines)
}

func detailLines(item Item) []string {
	detail := strings.TrimSpace(item.Detail)
	if detail == "" {
		detail = item.Subtitle
	}
	lines := wrapLines(detail, 1000)
	if len(lines) == 0 {
		return []string{"No detail for this row."}
	}
	return lines
}

func rowListLine(item Item, width int) string {
	width = maxInt(width, 1)
	title := item.Title
	if item.Depth > 0 {
		title = strings.Repeat("  ", minInt(item.Depth, 6)) + "-> " + title
	}
	if width < 46 {
		return truncateCells(title, width)
	}
	kind := rowKind(item)
	when := rowWhen(item)
	where := rowWhere(item)
	meta := strings.TrimSpace(joinNonEmpty([]string{where, when}, " "))
	kindW := minInt(maxInt(5, width/9), 10)
	metaW := minInt(maxInt(12, width/4), 28)
	titleW := maxInt(1, width-kindW-metaW-2)
	return padCells(truncateCells(kind, kindW), kindW) + " " +
		padCells(truncateCells(meta, metaW), metaW) + " " +
		truncateCells(title, titleW)
}

func rowKind(item Item) string {
	if kind := itemKind(item); kind != "" {
		return kind
	}
	return "row"
}

func itemKind(item Item) string {
	if strings.TrimSpace(item.Kind) != "" {
		return strings.TrimSpace(item.Kind)
	}
	for _, tag := range item.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			return tag
		}
	}
	return ""
}

func rowWhen(item Item) string {
	for _, value := range []string{item.UpdatedAt, item.CreatedAt} {
		if short := shortTimestamp(value); short != "" {
			return short
		}
	}
	for _, part := range subtitleParts(item.Subtitle) {
		if short := shortTimestamp(part); short != "" {
			return short
		}
	}
	return ""
}

func rowWhere(item Item) string {
	for _, value := range []string{item.Container, item.Scope, item.Author} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	kind := strings.ToLower(itemKind(item))
	for _, part := range subtitleParts(item.Subtitle) {
		lower := strings.ToLower(part)
		if lower == kind || shortTimestamp(part) != "" || looksMachineID(part) {
			continue
		}
		return part
	}
	return ""
}

func itemScope(item Item) string {
	if strings.TrimSpace(item.Scope) != "" {
		return strings.TrimSpace(item.Scope)
	}
	return fieldValue(item, "scope")
}

func itemContainer(item Item) string {
	if strings.TrimSpace(item.Container) != "" {
		return strings.TrimSpace(item.Container)
	}
	return firstNonEmpty(fieldValue(item, "container"), rowWhere(item))
}

func itemAuthor(item Item) string {
	if strings.TrimSpace(item.Author) != "" {
		return strings.TrimSpace(item.Author)
	}
	return firstNonEmpty(fieldValue(item, "author"), fieldValue(item, "user"), fieldValue(item, "sender"))
}

func fieldValue(item Item, keys ...string) string {
	if len(item.Fields) == 0 {
		return ""
	}
	for _, key := range keys {
		for actual, value := range item.Fields {
			if strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(key)) {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func itemSortTime(item Item) (time.Time, bool) {
	for _, value := range []string{item.UpdatedAt, item.CreatedAt} {
		if t, ok := parseTimestamp(value); ok {
			return t, true
		}
	}
	for _, part := range subtitleParts(item.Subtitle) {
		if t, ok := parseTimestamp(part); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func subtitleParts(subtitle string) []string {
	raw := strings.Split(subtitle, "  ")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func shortTimestamp(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if t, ok := parseTimestamp(value); ok {
		return t.UTC().Format("2006-01-02 15:04")
	}
	if len(value) >= len("2006-01-02") && value[4] == '-' && value[7] == '-' {
		return truncateCells(strings.ReplaceAll(value, "T", " "), 16)
	}
	return ""
}

func parseTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func looksMachineID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 12 || strings.ContainsAny(value, " \t\n") {
		return false
	}
	digits := 0
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 4
}

func titleStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(archiveHeaderFG)).
		Background(lipgloss.Color(archiveHeaderBG)).
		Width(width)
}

func mutedStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(archiveMutedFG)).
		Width(width)
}

func accentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(archiveSubtleAccentFG))
}

func tagStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(archiveSubtleAccentFG)).
		Width(width)
}

func rowStyle(width int, selected bool, focused bool) lipgloss.Style {
	style := lipgloss.NewStyle().Width(width)
	if selected {
		if focused {
			return style.
				Foreground(lipgloss.Color(archiveSelectedFG)).
				Background(lipgloss.Color(archiveSelectedBG))
		}
		return style.
			Foreground(lipgloss.Color(archiveBlurSelectedFG)).
			Background(lipgloss.Color(archiveBlurSelectedBG))
	}
	return style.Foreground(lipgloss.Color(archiveTextFG))
}

func separator(width int) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(archiveBorderColor)).
		Width(width).
		Render(strings.Repeat("-", minInt(width, 120)))
}

func truncateCells(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes))+3 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}

func wrap(value string, width int) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	var b strings.Builder
	for lipgloss.Width(value) > width {
		line := ansi.Cut(value, 0, width)
		cut := strings.LastIndex(line, " ")
		if cut > 0 {
			line = strings.TrimRight(line[:cut], " ")
		}
		if line == "" {
			line = ansi.Cut(value, 0, width)
		}
		b.WriteString(line)
		b.WriteByte('\n')
		value = strings.TrimSpace(strings.TrimPrefix(value, line))
	}
	b.WriteString(value)
	return b.String()
}

func wrapLines(value string, width int) []string {
	width = maxInt(width, 1)
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(wrap(line, width), "\n")...)
	}
	return out
}

func padCells(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) > width {
		value = truncateCells(value, width)
	}
	for lipgloss.Width(value) < width {
		value += " "
	}
	return value
}

func fitBlock(value string, width, height int) string {
	width = maxInt(width, 1)
	height = maxInt(height, 1)
	lines := strings.Split(value, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = padCells(truncateCells(line, width), width)
	}
	return strings.Join(lines, "\n")
}

func normalizeSourceKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SourceRemote:
		return SourceRemote
	default:
		return SourceLocal
	}
}

func footerPalette(source string) (lipgloss.Color, lipgloss.Color) {
	switch normalizeSourceKind(source) {
	case SourceRemote:
		return lipgloss.Color(archiveRemoteFooterBG), lipgloss.Color(archiveFooterFG)
	default:
		return lipgloss.Color(archiveLocalFooterBG), lipgloss.Color(archiveFooterFG)
	}
}

func strconvQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
