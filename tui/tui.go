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
	"github.com/charmbracelet/x/term"
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
	Text      string            `json:"text,omitempty"`
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
	Layout         LayoutPreset
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
		Layout:         layout,
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
		Text:      strings.TrimSpace(r.Text),
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
	model := newModel(opts)
	if width, height, err := term.GetSize(output.Fd()); err == nil && width > 0 && height > 0 {
		model.width = width
		model.height = height
		model.ensureVisible()
	}
	program := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),
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

func inferLayoutFromItems(items []Item) LayoutPreset {
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(itemKind(item))) {
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
	groups         []itemGroup
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
	layoutPreset   LayoutPreset
	sortMode       sortMode
	layoutMode     layoutMode
	menuOpen       bool
	menuTitle      string
	menuContext    paneFocus
	menuItems      []menuItem
	menuIndex      int
	menuOff        int
	menuFloating   bool
	menuRect       rect
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
	actionSeparator
	actionFocusRows
	actionFocusContext
	actionFocusDetail
	actionSortMenu
	actionHelpMenu
	actionClearFilter
	actionStartFilter
	actionToggleLayout
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

func (item menuItem) selectable() bool {
	return item.action != actionSeparator
}

func menuSection(label string) menuItem {
	return menuItem{label: label, action: actionSeparator}
}

type layoutMode string

const (
	layoutModeColumns    layoutMode = "columns"
	layoutModeRightStack layoutMode = "right-stack"
)

func newModel(opts Options) model {
	layout := opts.Layout
	if layout == LayoutAuto {
		layout = inferLayoutFromItems(opts.Items)
	}
	m := model{
		title:          strings.TrimSpace(opts.Title),
		items:          append([]Item(nil), opts.Items...),
		width:          100,
		height:         30,
		focus:          focusRows,
		sourceKind:     normalizeSourceKind(opts.SourceKind),
		sourceLocation: strings.TrimSpace(opts.SourceLocation),
		layoutPreset:   layout,
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
	mode    string
}

type itemGroup struct {
	Key     string
	Title   string
	Kind    string
	Scope   string
	Count   int
	Latest  string
	Members []int
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
		if typed.Action == tea.MouseActionMotion && typed.Button == tea.MouseButtonNone {
			if m.menuOpen {
				m.handleMenuMouse(typed)
			}
			return m, nil
		}
		if m.menuOpen {
			m.handleMenuMouse(typed)
			return m, nil
		}
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
			case "ctrl+c", "ctrl+d", "q":
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
		case "ctrl+c", "ctrl+d", "q":
			return m, tea.Quit
		case "tab", "right":
			m.focus = nextFocus(m.focus, 1)
		case "shift+tab", "left":
			m.focus = nextFocus(m.focus, -1)
		case "up", "k":
			if m.focus == focusRows {
				m.moveGroup(-1)
			} else if m.focus == focusContext {
				m.moveMember(-1)
			} else {
				m.scrollFocused(-1)
			}
		case "down", "j":
			if m.focus == focusRows {
				m.moveGroup(1)
			} else if m.focus == focusContext {
				m.moveMember(1)
			} else {
				m.scrollFocused(1)
			}
		case "pgup", "ctrl+b":
			if m.focus == focusRows {
				m.moveGroup(-m.pageSize())
			} else if m.focus == focusContext {
				m.moveMember(-m.focusedPageSize())
			} else {
				m.scrollFocused(-m.focusedPageSize())
			}
		case "pgdown", "ctrl+f":
			if m.focus == focusRows {
				m.moveGroup(m.pageSize())
			} else if m.focus == focusContext {
				m.moveMember(m.focusedPageSize())
			} else {
				m.scrollFocused(m.focusedPageSize())
			}
		case "home", "g":
			if m.focus == focusRows {
				m.selectGroup(0)
				m.ensureVisible()
			} else if m.focus == focusContext {
				m.selectMemberOffset(0)
			}
		case "end", "G":
			if m.focus == focusRows {
				m.selectGroup(len(m.groups) - 1)
				m.ensureVisible()
			} else if m.focus == focusContext {
				m.selectMemberOffset(len(m.currentGroupMembers()) - 1)
			}
		case "/", "f":
			m.startFilter()
		case "s":
			m.openSortMenu()
		case "m":
			m.openActionMenu()
		case "?":
			m.openHelpMenu()
		case "l":
			m.toggleLayout()
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
	m.closeMenu()
	focus := m.paneAt(x, y)
	m.focus = focus
	if focus == focusRows {
		m.selectGroupAt(layout.rows, x, y)
	} else if focus == focusContext {
		m.selectMemberAt(layout.context, x, y)
	}
}

func (m *model) handleRightClick(x, y int) {
	focus := m.paneAt(x, y)
	m.focus = focus
	if focus == focusRows {
		m.selectGroupAt(m.layout().rows, x, y)
	} else if focus == focusContext {
		m.selectMemberAt(m.layout().context, x, y)
	}
	m.openActionMenuFor(focus)
	m.placeFloatingMenu(x, y)
}

func (m *model) selectGroupAt(rect rect, x, y int) {
	row := y - rect.y - 3
	if row == -1 {
		m.sortRowsFromHeader(x - rect.x - 2)
		return
	}
	if row < 0 || row >= rowsViewportHeight(rect.h) {
		return
	}
	groupIndex := m.offset + row
	if groupIndex < 0 || groupIndex >= len(m.groups) {
		return
	}
	m.selectGroup(groupIndex)
}

func (m *model) selectMemberAt(rect rect, x, y int) {
	row := y - rect.y - 3
	if row == -1 {
		m.sortRowsFromHeader(x - rect.x - 2)
		return
	}
	members := m.currentGroupMembers()
	memberOffset := m.contextOffset + row
	if row < 0 || row >= rowsViewportHeight(rect.h) || memberOffset < 0 || memberOffset >= len(members) {
		return
	}
	itemIndex := members[memberOffset]
	m.selectItemIndex(itemIndex)
	m.detailOffset = 0
	m.ensureVisible()
}

func (m *model) selectGroup(groupIndex int) {
	if groupIndex < 0 || groupIndex >= len(m.groups) || len(m.groups[groupIndex].Members) == 0 {
		return
	}
	m.selectItemIndex(m.groups[groupIndex].Members[0])
	m.contextOffset = 0
	m.detailOffset = 0
	m.ensureVisible()
}

func (m *model) handleMenuMouse(msg tea.MouseMsg) {
	switch {
	case msg.Type == tea.MouseWheelUp || msg.Button == tea.MouseButtonWheelUp:
		m.menuIndex = m.nextSelectableMenuIndex(-1)
		m.keepMenuVisible()
		return
	case msg.Type == tea.MouseWheelDown || msg.Button == tea.MouseButtonWheelDown:
		m.menuIndex = m.nextSelectableMenuIndex(1)
		m.keepMenuVisible()
		return
	case msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress:
		m.closeMenu()
		return
	}
	index, ok := m.menuIndexAtMouse(msg.X, msg.Y)
	if msg.Action == tea.MouseActionMotion {
		if !ok || index < 0 || index >= len(m.menuItems) {
			return
		}
		m.menuIndex = m.nearestSelectableMenuIndex(index, 1)
		m.keepMenuVisible()
		return
	}
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return
	}
	if !ok {
		m.closeMenu()
		return
	}
	if index < 0 || index >= len(m.menuItems) {
		return
	}
	if !m.menuItems[index].selectable() {
		m.menuIndex = m.nearestSelectableMenuIndex(index, 1)
		m.keepMenuVisible()
		return
	}
	m.menuIndex = index
	m.keepMenuVisible()
	_ = m.runMenuAction(m.menuItems[m.menuIndex].action)
}

func (m model) menuIndexAtMouse(x, y int) (int, bool) {
	menuRect := m.layout().detail
	rowOffset := 4
	if m.menuFloating {
		menuRect = m.menuRect
	}
	if !menuRect.contains(x, y) {
		return 0, false
	}
	return m.menuOff + y - menuRect.y - rowOffset, true
}

func (m *model) updateMenuKey(key tea.KeyMsg) tea.Cmd {
	page := maxInt(1, m.menuVisibleCount())
	if index, ok := visibleMenuShortcutIndex(key.String(), m.menuItems, m.menuOff, page); ok {
		m.menuIndex = index
		return m.runMenuAction(m.menuItems[m.menuIndex].action)
	}
	switch key.String() {
	case "ctrl+c":
		return tea.Quit
	case "q", "ctrl+d":
		return tea.Quit
	case "esc":
		m.closeMenu()
	case "up", "k":
		m.menuIndex = m.nextSelectableMenuIndex(-1)
		m.keepMenuVisible()
	case "down", "j":
		m.menuIndex = m.nextSelectableMenuIndex(1)
		m.keepMenuVisible()
	case "pgup", "ctrl+b":
		m.menuIndex = m.nearestSelectableMenuIndex(m.menuIndex-page, -1)
		m.keepMenuVisible()
	case "pgdown", "ctrl+f":
		m.menuIndex = m.nearestSelectableMenuIndex(m.menuIndex+page, 1)
		m.keepMenuVisible()
	case "home", "g":
		m.menuIndex = m.firstSelectableMenuIndex()
		m.keepMenuVisible()
	case "end", "G":
		m.menuIndex = m.lastSelectableMenuIndex()
		m.keepMenuVisible()
	case "enter", " ":
		if len(m.menuItems) > 0 {
			return m.runMenuAction(m.menuItems[m.menuIndex].action)
		}
	case "s":
		m.openSortMenu()
	case "?":
		m.openHelpMenu()
	case "/":
		m.startFilter()
	case "l":
		m.toggleLayout()
	}
	return nil
}

func (m *model) openActionMenu() {
	m.openActionMenuFor(m.focus)
}

func (m *model) openActionMenuFor(context paneFocus) {
	items := []menuItem{
		menuSection("Pane"),
		{label: "Focus rows pane", action: actionFocusRows},
		{label: "Focus context pane", action: actionFocusContext},
		{label: "Focus detail pane", action: actionFocusDetail},
		menuSection("View"),
		{label: "Sort rows", action: actionSortMenu},
		{label: "Filter rows...", action: actionStartFilter},
		{label: "Toggle wide layout", action: actionToggleLayout},
	}
	if m.query != "" {
		items = append(items, menuItem{label: "Clear filter", action: actionClearFilter})
	}
	items = append(items,
		menuItem{label: "Help", action: actionHelpMenu},
		menuItem{label: "Close menu", action: actionClose},
	)
	m.menuContext = context
	m.openMenu("Actions", items)
}

func (m *model) openSortMenu() {
	m.openMenu("Sort", []menuItem{
		menuSection("Order"),
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
		menuSection("Mouse"),
		{label: "Tab/arrow: select pane", action: actionClose},
		{label: "Mouse click: select pane/row", action: actionClose},
		{label: "Right click or m: floating actions", action: actionClose},
		{label: "Click row header: sort", action: actionClose},
		menuSection("Keyboard"),
		{label: "s: sort rows", action: actionClose},
		{label: "l: toggle layout", action: actionClose},
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
	m.menuIndex = m.firstSelectableMenuIndex()
	m.menuOff = 0
	m.filterMode = false
	m.keepMenuVisible()
}

func (m *model) closeMenu() {
	m.menuOpen = false
	m.menuTitle = ""
	m.menuItems = nil
	m.menuIndex = 0
	m.menuOff = 0
	m.menuFloating = false
	m.menuRect = rect{}
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
	case actionStartFilter:
		m.startFilter()
	case actionToggleLayout:
		m.toggleLayout()
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

func (m *model) startFilter() {
	m.closeMenu()
	m.filterMode = true
}

func (m *model) toggleLayout() {
	if m.layoutMode == layoutModeRightStack {
		m.layoutMode = layoutModeColumns
		return
	}
	m.layoutMode = layoutModeRightStack
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
		if layout.context.x == 0 {
			body = lipgloss.JoinVertical(lipgloss.Left, rows, context, detail)
		} else {
			top := lipgloss.JoinHorizontal(lipgloss.Top, rows, context)
			body = lipgloss.JoinVertical(lipgloss.Left, top, detail)
		}
	} else {
		if layout.detail.y > layout.context.y {
			right := lipgloss.JoinVertical(lipgloss.Left, context, detail)
			body = lipgloss.JoinHorizontal(lipgloss.Top, rows, right)
		} else {
			body = lipgloss.JoinHorizontal(lipgloss.Top, rows, context, detail)
		}
	}
	body = fitBlock(body, width, maxInt(1, layout.footerY()-1))
	view := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if m.menuOpen && m.menuFloating {
		view = m.renderFloatingMenu(view)
	}
	return fitBlock(view, width, height)
}

func (m model) renderHeader(width int) string {
	status := fmt.Sprintf("%d/%d rows", len(m.filtered), len(m.items))
	if m.query != "" {
		status += " filtered by " + strconvQuote(m.query)
	}
	status += "  sort:" + m.sortMode.Label()
	status += "  layout:" + m.layout().mode
	line := m.title + "  " + status
	if m.filterMode {
		line += "  filter> " + m.query
	} else if m.menuOpen {
		line += "  menu> " + m.menuTitle
	}
	return titleStyle(width).Render(padCells(" "+truncateCells(line, maxInt(1, width-2)), width))
}

func (m model) renderRowsPane(rect rect) string {
	lines := []string{groupListHeader(paneContentWidth(rect.w), m.sortMode)}
	if m.filterMode {
		lines = append(lines, accentStyle().Render("filter> ")+m.query)
	}
	if len(m.groups) == 0 {
		lines = append(lines, mutedStyle(rect.w).Render("no rows match"))
	} else {
		current := m.currentGroupIndex()
		for _, index := range m.visibleGroups() {
			group := m.groups[index]
			selected := index == current
			prefix := "  "
			if selected {
				prefix = "> "
			}
			line := prefix + groupListLine(group, paneContentWidth(rect.w)-lipgloss.Width(prefix))
			lines = append(lines, rowStyle(paneContentWidth(rect.w), selected, m.focus == focusRows).Render(line))
		}
	}
	return pane(m.groupPaneTitle(), m.groupPositionLabel(), lines, rect, focusRows, m.focus, rowsPaneAccent)
}

func (m model) renderContextPane(rect rect) string {
	group, ok := m.currentGroup()
	if !ok {
		return pane(m.memberPaneTitle(), "", []string{"No group selected."}, rect, focusContext, m.focus, contextPaneAccent)
	}
	lines := []string{rowListHeader(paneContentWidth(rect.w), m.sortMode)}
	members := m.currentGroupMembers()
	if len(members) == 0 {
		lines = append(lines, mutedStyle(rect.w).Render("no rows in group"))
	} else {
		selectedItem := m.currentItemIndex()
		start := clampInt(m.contextOffset, 0, maxInt(0, len(members)-rowsViewportHeight(rect.h)))
		end := minInt(len(members), start+rowsViewportHeight(rect.h))
		for _, itemIndex := range members[start:end] {
			item := m.items[itemIndex]
			selected := itemIndex == selectedItem
			prefix := "  "
			if selected {
				prefix = "> "
			}
			line := prefix + rowListLine(item, paneContentWidth(rect.w)-lipgloss.Width(prefix))
			lines = append(lines, rowStyle(paneContentWidth(rect.w), selected, m.focus == focusContext).Render(line))
		}
	}
	return pane(m.memberPaneTitle(), group.Title, lines, rect, focusContext, m.focus, contextPaneAccent)
}

func (m model) renderDetailPane(rect rect) string {
	if m.menuOpen && !m.menuFloating {
		return pane(m.menuTitle, "enter/1-9 choose  esc close", m.menuLines(paneContentWidth(rect.w)), rect, focusDetail, m.focus, detailPaneAccent)
	}
	item, ok := m.selectedItem()
	if !ok {
		return pane("Detail", "", []string{"No row selected."}, rect, focusDetail, m.focus, detailPaneAccent)
	}
	lines := m.detailLines(item)
	return paneScrolled(m.detailPaneTitle(), paneFocusLabel(m.focus == focusDetail), lines, rect, focusDetail, m.focus, detailPaneAccent, m.detailOffset)
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
	controls := footerControls(width)
	bg, fg := footerPalette(m.sourceKind)
	statusLine := padCells(" "+truncateCells(line, maxInt(1, width-2)), width)
	controlsLine := padCells(" "+truncateCells(controls, maxInt(1, width-2)), width)
	return lipgloss.NewStyle().Width(width).Height(2).Background(bg).Foreground(fg).Render(statusLine + "\n" + controlsLine)
}

func (m model) menuLines(width int) []string {
	if len(m.menuItems) == 0 {
		return []string{"No actions."}
	}
	palette := actionMenuColors(m.menuContext)
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.accent)).Render(firstNonEmpty(m.menuTitle, "Actions"))
	lines := []string{title, dim(actionMenuSubtitle(m.menuContext)), ""}
	visible := m.menuVisibleCount()
	start := clampInt(m.menuOff, 0, maxInt(0, len(m.menuItems)-visible))
	end := minInt(len(m.menuItems), start+visible)
	shortcut := 0
	for i := start; i < end; i++ {
		item := m.menuItems[i]
		if !item.selectable() {
			lines = append(lines, truncateCells("  "+dim(item.label), width))
			continue
		}
		shortcut++
		prefix := "  "
		if i == m.menuIndex {
			prefix = "> "
		}
		key := "   "
		if shortcut <= 9 {
			key = fmt.Sprintf("%d. ", shortcut)
		}
		line := truncateCells(prefix+key+item.label, width)
		if i == m.menuIndex {
			line = selectedMenuLineStyle(width, palette).Render(padCells(line, width))
		}
		lines = append(lines, line)
	}
	footer := "Enter/1-9 run  Esc close"
	if len(m.menuItems) > visible {
		footer = fmt.Sprintf("%s  Pg page  %d-%d/%d", footer, start+1, end, len(m.menuItems))
	}
	lines = append(lines, "", dim(footer))
	return lines
}

type actionMenuPalette struct {
	accent     string
	background string
	foreground string
	selectedBG string
	selectedFG string
}

func actionMenuColors(context paneFocus) actionMenuPalette {
	switch context {
	case focusRows:
		return actionMenuPalette{
			accent:     rowsPaneAccent,
			background: "#111827",
			foreground: "#d7dee8",
			selectedBG: "#2f3f56",
			selectedFG: "#f8fafc",
		}
	case focusContext:
		return actionMenuPalette{
			accent:     contextPaneAccent,
			background: "#111a16",
			foreground: "#d7dee8",
			selectedBG: "#344337",
			selectedFG: "#f8fafc",
		}
	default:
		return actionMenuPalette{
			accent:     detailPaneAccent,
			background: "#151922",
			foreground: "#d7dee8",
			selectedBG: "#3f3a31",
			selectedFG: "#f8fafc",
		}
	}
}

func actionMenuSubtitle(context paneFocus) string {
	switch context {
	case focusRows:
		return "row scope"
	case focusContext:
		return "context scope"
	case focusDetail:
		return "detail scope"
	default:
		return "current selection"
	}
}

func (m model) renderFloatingMenu(view string) string {
	if m.menuRect.w <= 0 || m.menuRect.h <= 0 {
		return view
	}
	lines := m.menuLines(maxInt(1, m.menuRect.w-2))
	if len(lines) > maxInt(0, m.menuRect.h-2) {
		lines = lines[:maxInt(0, m.menuRect.h-2)]
	}
	box := floatingMenuStyle(m.menuRect.w, m.menuRect.h, actionMenuColors(m.menuContext)).Render(strings.Join(lines, "\n"))
	return overlayBlock(view, box, m.menuRect.x, m.menuRect.y, m.width)
}

func (m *model) placeFloatingMenu(x, y int) {
	if !m.menuOpen {
		return
	}
	maxWidth := maxInt(24, m.width-2)
	width := clampInt(m.preferredMenuWidth(), 34, minInt(58, maxWidth))
	availableHeight := maxInt(1, m.height-3)
	visibleRows := minInt(maxInt(1, len(m.menuItems)), 12)
	height := minInt(visibleRows+7, availableHeight)
	if height < minInt(8, availableHeight) {
		height = minInt(8, availableHeight)
	}
	maxX := maxInt(0, m.width-width)
	minY := 1
	maxY := maxInt(minY, m.height-2-height)
	m.menuFloating = true
	m.menuRect = rect{
		x: clampInt(x+1, 0, maxX),
		y: clampInt(y, minY, maxY),
		w: width,
		h: height,
	}
	m.keepMenuVisible()
}

func (m model) preferredMenuWidth() int {
	width := lipgloss.Width(firstNonEmpty(m.menuTitle, "Actions")) + 4
	for _, item := range m.menuItems {
		width = maxInt(width, lipgloss.Width(item.label)+8)
	}
	return width
}

func (m model) menuVisibleCount() int {
	if m.menuFloating && m.menuRect.h > 0 {
		return maxInt(1, m.menuRect.h-7)
	}
	return maxInt(1, m.layout().detail.h-7)
}

func visibleMenuShortcutIndex(key string, items []menuItem, menuOff, visible int) (int, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0, false
	}
	target := int(key[0] - '0')
	shortcut := 0
	end := minInt(len(items), menuOff+maxInt(1, visible))
	for index := menuOff; index < end; index++ {
		if !items[index].selectable() {
			continue
		}
		shortcut++
		if shortcut == target {
			return index, true
		}
	}
	return 0, false
}

func (m model) firstSelectableMenuIndex() int {
	for index, item := range m.menuItems {
		if item.selectable() {
			return index
		}
	}
	return 0
}

func (m model) lastSelectableMenuIndex() int {
	for index := len(m.menuItems) - 1; index >= 0; index-- {
		if m.menuItems[index].selectable() {
			return index
		}
	}
	return maxInt(0, len(m.menuItems)-1)
}

func (m model) nextSelectableMenuIndex(delta int) int {
	if delta == 0 || len(m.menuItems) == 0 {
		return m.menuIndex
	}
	for index := m.menuIndex + delta; index >= 0 && index < len(m.menuItems); index += delta {
		if m.menuItems[index].selectable() {
			return index
		}
	}
	return m.menuIndex
}

func (m model) nearestSelectableMenuIndex(index, direction int) int {
	if len(m.menuItems) == 0 {
		return 0
	}
	index = clampInt(index, 0, len(m.menuItems)-1)
	if m.menuItems[index].selectable() {
		return index
	}
	if direction == 0 {
		direction = 1
	}
	for next := index + direction; next >= 0 && next < len(m.menuItems); next += direction {
		if m.menuItems[next].selectable() {
			return next
		}
	}
	return m.firstSelectableMenuIndex()
}

func (m *model) keepMenuVisible() {
	if len(m.menuItems) == 0 {
		m.menuOff = 0
		return
	}
	visible := m.menuVisibleCount()
	m.menuIndex = m.nearestSelectableMenuIndex(m.menuIndex, 1)
	if m.menuIndex < m.menuOff {
		m.menuOff = m.menuIndex
	}
	if m.menuIndex >= m.menuOff+visible {
		m.menuOff = m.menuIndex - visible + 1
	}
	m.menuOff = clampInt(m.menuOff, 0, maxInt(0, len(m.menuItems)-visible))
}

func footerControls(width int) string {
	full := "Tab focus  click select  header sort  right-click menu  m actions  s sort  l layout  wheel scroll  / filter  ? help  q quit"
	if lipgloss.Width(full) <= maxInt(1, width-2) {
		return full
	}
	compact := "Tab focus  click select  right-click menu  s sort  / filter  ? help  q quit"
	if lipgloss.Width(compact) <= maxInt(1, width-2) {
		return compact
	}
	return "Tab focus click menu s sort / filter ? help q quit"
}

func (m model) footerLocation() string {
	location := strings.TrimSpace(m.sourceLocation)
	if location == "" {
		return m.sourceKind
	}
	return m.sourceKind + " " + location
}

func (m *model) moveGroup(delta int) {
	if len(m.groups) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	m.selectGroup(clampInt(m.currentGroupIndex()+delta, 0, len(m.groups)-1))
}

func (m *model) moveMember(delta int) {
	members := m.currentGroupMembers()
	if len(members) == 0 {
		return
	}
	current := m.currentMemberOffset()
	m.selectMemberOffset(clampInt(current+delta, 0, len(members)-1))
}

func (m *model) selectMemberOffset(offset int) {
	members := m.currentGroupMembers()
	if len(members) == 0 {
		return
	}
	offset = clampInt(offset, 0, len(members)-1)
	m.selectItemIndex(members[offset])
	m.contextOffset = 0
	m.detailOffset = 0
	m.ensureVisible()
}

func (m *model) scrollFocused(delta int) {
	switch m.focus {
	case focusContext:
		m.moveMember(delta)
	case focusDetail:
		m.detailOffset = clampInt(m.detailOffset+delta, 0, m.maxDetailOffset())
	default:
		m.moveGroup(delta)
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
		return maxInt(1, rowsViewportHeight(layout.context.h))
	case focusDetail:
		return maxInt(1, paneContentHeight(layout.detail.h))
	default:
		return m.pageSize()
	}
}

func (m model) maxContextOffset() int {
	return maxInt(0, len(m.currentGroupMembers())-rowsViewportHeight(m.layout().context.h))
}

func (m model) maxDetailOffset() int {
	item, ok := m.selectedItem()
	if !ok {
		return 0
	}
	layout := m.layout()
	return maxPaneScroll(m.detailLines(item), layout.detail)
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
	m.buildGroups()
	if len(m.filtered) == 0 {
		m.selected = 0
		m.offset = 0
		m.contextOffset = 0
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

func (m *model) buildGroups() {
	byKey := make(map[string]int)
	groups := make([]itemGroup, 0)
	for _, itemIndex := range m.filtered {
		if itemIndex < 0 || itemIndex >= len(m.items) {
			continue
		}
		item := m.items[itemIndex]
		key, title, kind, scope := m.groupFields(item)
		groupIndex, ok := byKey[key]
		if !ok {
			groupIndex = len(groups)
			byKey[key] = groupIndex
			groups = append(groups, itemGroup{
				Key:   key,
				Title: title,
				Kind:  kind,
				Scope: scope,
			})
		}
		group := &groups[groupIndex]
		group.Members = append(group.Members, itemIndex)
		group.Count++
		if newerTimestamp(item.UpdatedAt, group.Latest) {
			group.Latest = item.UpdatedAt
		}
		if newerTimestamp(item.CreatedAt, group.Latest) {
			group.Latest = item.CreatedAt
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if less, ok := compareGroups(groups[i], groups[j], m.sortMode); ok {
			return less
		}
		return strings.ToLower(groups[i].Title) < strings.ToLower(groups[j].Title)
	})
	for index := range groups {
		m.sortGroupMembers(groups[index].Members)
	}
	m.groups = groups
}

func (m model) sortGroupMembers(members []int) {
	if len(members) < 2 {
		return
	}
	sort.SliceStable(members, func(i, j int) bool {
		left := m.items[members[i]]
		right := m.items[members[j]]
		switch m.sortMode {
		case sortNewest:
			if less, ok := compareItemTime(left, right, true); ok {
				return less
			}
		case sortOldest:
			if less, ok := compareItemTime(left, right, false); ok {
				return less
			}
		case sortTitle:
			if less, ok := compareStrings(left.Title, right.Title); ok {
				return less
			}
		case sortKind:
			if less, ok := compareStrings(itemKind(left), itemKind(right)); ok {
				return less
			}
		case sortScope, sortContainer:
			if less, ok := compareStrings(itemContainer(left), itemContainer(right)); ok {
				return less
			}
		case sortAuthor:
			if less, ok := compareStrings(itemAuthor(left), itemAuthor(right)); ok {
				return less
			}
		default:
			if m.layoutPreset == LayoutChat {
				if less, ok := compareItemTime(left, right, false); ok {
					return less
				}
			}
			if m.layoutPreset == LayoutDocument {
				if less, ok := compareItemTime(left, right, true); ok {
					return less
				}
			}
		}
		return members[i] < members[j]
	})
}

func (m model) groupFields(item Item) (key, title, kind, scope string) {
	switch m.layoutPreset {
	case LayoutChat:
		if container := strings.TrimSpace(item.Container); container != "" {
			return "container:" + container, container, "channel", strings.TrimSpace(item.Scope)
		}
		if author := strings.TrimSpace(item.Author); author != "" {
			return "author:" + author, author, "person", strings.TrimSpace(item.Scope)
		}
	case LayoutDocument:
		if parent := strings.TrimSpace(item.ParentID); parent != "" {
			return "parent:" + parent, parent, "parent", strings.TrimSpace(item.Scope)
		}
		if container := strings.TrimSpace(item.Container); container != "" {
			return "container:" + container, container, "database", strings.TrimSpace(item.Scope)
		}
	}
	for _, value := range []struct {
		prefix string
		title  string
		kind   string
	}{
		{"container:", strings.TrimSpace(item.Container), "container"},
		{"scope:", strings.TrimSpace(item.Scope), "scope"},
		{"author:", strings.TrimSpace(item.Author), "person"},
	} {
		if value.title != "" {
			return value.prefix + value.title, value.title, value.kind, strings.TrimSpace(item.Scope)
		}
	}
	title = firstNonEmpty(item.Title, item.ID, itemKind(item), "row")
	return "row:" + title, title, firstNonEmpty(itemKind(item), "row"), strings.TrimSpace(item.Scope)
}

func compareGroups(left, right itemGroup, mode sortMode) (bool, bool) {
	switch mode {
	case sortNewest:
		return compareGroupTime(left, right, true)
	case sortOldest:
		return compareGroupTime(left, right, false)
	case sortTitle, sortContainer:
		return compareStrings(left.Title, right.Title)
	case sortKind:
		return compareStrings(left.Kind, right.Kind)
	case sortScope:
		return compareStrings(left.Scope, right.Scope)
	case sortAuthor:
		return compareStrings(left.Title, right.Title)
	default:
		if less, ok := compareGroupTime(left, right, true); ok {
			return less, true
		}
		return false, false
	}
}

func compareGroupTime(left, right itemGroup, newest bool) (bool, bool) {
	leftTime, leftOK := parseTimestamp(left.Latest)
	rightTime, rightOK := parseTimestamp(right.Latest)
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

func newerTimestamp(candidate, current string) bool {
	candidateTime, candidateOK := parseTimestamp(candidate)
	if !candidateOK {
		return false
	}
	currentTime, currentOK := parseTimestamp(current)
	return !currentOK || candidateTime.After(currentTime)
}

func (m *model) ensureVisible() {
	page := m.pageSize()
	groupIndex := m.currentGroupIndex()
	if groupIndex < m.offset {
		m.offset = groupIndex
	}
	if groupIndex >= m.offset+page {
		m.offset = groupIndex - page + 1
	}
	m.offset = clampInt(m.offset, 0, maxInt(len(m.groups)-1, 0))
	memberPage := rowsViewportHeight(m.layout().context.h)
	memberIndex := m.currentMemberOffset()
	if memberIndex < m.contextOffset {
		m.contextOffset = memberIndex
	}
	if memberIndex >= m.contextOffset+memberPage {
		m.contextOffset = memberIndex - memberPage + 1
	}
	m.contextOffset = clampInt(m.contextOffset, 0, m.maxContextOffset())
}

func (m model) pageSize() int {
	return maxInt(1, rowsViewportHeight(m.layout().rows.h))
}

func (m model) visibleRows() []int {
	end := minInt(len(m.filtered), m.offset+m.pageSize())
	out := make([]int, 0, end-m.offset)
	for i := m.offset; i < end; i++ {
		out = append(out, i)
	}
	return out
}

func (m model) visibleGroups() []int {
	end := minInt(len(m.groups), m.offset+m.pageSize())
	out := make([]int, 0, maxInt(0, end-m.offset))
	for i := m.offset; i < end; i++ {
		out = append(out, i)
	}
	return out
}

func (m model) layout() archiveLayout {
	width := maxInt(m.width, 80)
	height := maxInt(m.height, 16)
	bodyH := maxInt(8, height-3)
	if width >= 140 {
		if m.layoutMode == layoutModeRightStack {
			rowsW := maxInt(56, width*44/100)
			rightW := width - rowsW
			contextH := maxInt(8, bodyH*42/100)
			return archiveLayout{
				rows:    rect{x: 0, y: 1, w: rowsW, h: bodyH},
				context: rect{x: rowsW, y: 1, w: rightW, h: contextH},
				detail:  rect{x: rowsW, y: 1 + contextH, w: rightW, h: bodyH - contextH},
				mode:    string(layoutModeRightStack),
			}
		}
		rowsW := maxInt(48, width*34/100)
		contextW := maxInt(40, width*28/100)
		detailW := width - rowsW - contextW
		if detailW < 42 {
			detailW = 42
			contextW = maxInt(30, width-rowsW-detailW)
		}
		return archiveLayout{
			rows:    rect{x: 0, y: 1, w: rowsW, h: bodyH},
			context: rect{x: rowsW, y: 1, w: contextW, h: bodyH},
			detail:  rect{x: rowsW + contextW, y: 1, w: width - rowsW - contextW, h: bodyH},
			mode:    string(layoutModeColumns),
		}
	}
	if width >= 100 {
		topH := maxInt(8, bodyH/2)
		rowsW := width / 2
		return archiveLayout{
			rows:    rect{x: 0, y: 1, w: rowsW, h: topH},
			context: rect{x: rowsW, y: 1, w: width - rowsW, h: topH},
			detail:  rect{x: 0, y: 1 + topH, w: width, h: bodyH - topH},
			stacked: true,
			mode:    "split",
		}
	}
	rowsH := minInt(maxInt(5, bodyH*36/100), maxInt(3, bodyH-6))
	contextH := minInt(maxInt(4, bodyH*28/100), maxInt(3, bodyH-rowsH-3))
	detailH := maxInt(3, bodyH-rowsH-contextH)
	return archiveLayout{
		rows:    rect{x: 0, y: 1, w: width, h: rowsH},
		context: rect{x: 0, y: 1 + rowsH, w: width, h: contextH},
		detail:  rect{x: 0, y: 1 + rowsH + contextH, w: width, h: detailH},
		stacked: true,
		mode:    "stacked",
	}
}

func (l archiveLayout) footerY() int {
	return maxInt(l.rows.y+l.rows.h, maxInt(l.context.y+l.context.h, l.detail.y+l.detail.h))
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

func (m model) currentGroup() (itemGroup, bool) {
	index := m.currentGroupIndex()
	if index < 0 || index >= len(m.groups) {
		return itemGroup{}, false
	}
	return m.groups[index], true
}

func (m model) currentGroupIndex() int {
	itemIndex := m.currentItemIndex()
	if itemIndex < 0 {
		if len(m.groups) == 0 {
			return 0
		}
		return clampInt(m.offset, 0, len(m.groups)-1)
	}
	for groupIndex, group := range m.groups {
		for _, member := range group.Members {
			if member == itemIndex {
				return groupIndex
			}
		}
	}
	return 0
}

func (m model) currentGroupMembers() []int {
	group, ok := m.currentGroup()
	if !ok {
		return nil
	}
	return group.Members
}

func (m model) currentMemberOffset() int {
	itemIndex := m.currentItemIndex()
	members := m.currentGroupMembers()
	for index, member := range members {
		if member == itemIndex {
			return index
		}
	}
	return 0
}

func (m *model) selectItemIndex(itemIndex int) {
	for index, filteredIndex := range m.filtered {
		if filteredIndex == itemIndex {
			m.selected = index
			return
		}
	}
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

func (m model) groupPositionLabel() string {
	if len(m.groups) == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d groups", m.currentGroupIndex()+1, len(m.groups))
}

func (m model) groupPaneTitle() string {
	switch m.layoutPreset {
	case LayoutChat:
		return "Channels / People"
	case LayoutDocument:
		return "Parents"
	default:
		return "Groups"
	}
}

func (m model) memberPaneTitle() string {
	switch m.layoutPreset {
	case LayoutChat:
		return "Messages"
	case LayoutDocument:
		return "Pages / Databases"
	default:
		return "Items"
	}
}

func (m model) detailPaneTitle() string {
	if m.layoutPreset == LayoutChat {
		return "Thread"
	}
	return "Detail"
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

func paneTitle(pane, focus paneFocus, suffix string) string {
	label := map[paneFocus]string{
		focusRows:    "Rows",
		focusContext: "Context",
		focusDetail:  "Detail",
	}[pane]
	if strings.TrimSpace(suffix) != "" && strings.TrimSpace(suffix) != label {
		label += " " + suffix
	}
	prefix := "[ ] "
	if pane == focus {
		prefix = "[*] "
	}
	return bold(prefix + label)
}

func paneStyle(pane, focus paneFocus, width, height int, accent string) lipgloss.Style {
	borderColor := accent
	if pane == focus {
		borderColor = "#f7f7ff"
	}
	return lipgloss.NewStyle().
		Width(maxInt(1, width-2)).
		Height(maxInt(1, height-2)).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Foreground(lipgloss.Color(archiveTextFG)).
		Padding(0, 1)
}

func pane(title, subtitle string, lines []string, rect rect, paneFocus paneFocus, focus paneFocus, accent string) string {
	return paneScrolled(title, subtitle, lines, rect, paneFocus, focus, accent, 0)
}

func paneScrolled(title, subtitle string, lines []string, rect rect, paneFocus paneFocus, focus paneFocus, accent string, scrollOffset int) string {
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
	titleLine := title
	if strings.TrimSpace(subtitle) != "" {
		titleLine += "  " + subtitle
	}
	header := paneTitle(paneFocus, focus, titleLine)
	body = append([]string(nil), body[scrollOffset:minInt(len(body), scrollOffset+contentH)]...)
	for len(body) < contentH {
		body = append(body, "")
	}
	out := append([]string{header}, body[:contentH]...)
	return paneStyle(paneFocus, focus, width, height, accent).Render(strings.Join(out, "\n"))
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
	return maxInt(1, width-4)
}

func paneContentHeight(height int) int {
	return maxInt(1, height-3)
}

func rowsViewportHeight(height int) int {
	return maxInt(1, paneContentHeight(height)-1)
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
		item.Text,
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

func (m model) detailLines(item Item) []string {
	switch m.layoutPreset {
	case LayoutChat:
		return m.chatDetailLines(item)
	case LayoutDocument:
		return documentDetailLines(item)
	}
	return genericDetailLines(item)
}

func genericDetailLines(item Item) []string {
	detail := strings.TrimSpace(item.Detail)
	var lines []string
	context := detailContextLines(item, true)
	if len(context) > 0 {
		lines = append(lines, "Context")
		lines = append(lines, context...)
	}
	if detail == "" {
		detail = item.Subtitle
	}
	if detail != "" {
		lines = append(lines, "", "Content")
		lines = append(lines, wrapLines(detail, 1000)...)
	}
	if len(lines) == 0 {
		lines = append(lines, "", "No detail for this row.")
	}
	return lines
}

func (m model) chatDetailLines(item Item) []string {
	var lines []string
	if header := chatHeaderLine(item); header != "" {
		lines = append(lines, header)
	}
	if meta := chatMetaLine(item); meta != "" {
		lines = append(lines, dim(meta))
	}
	if thread := m.threadLines(item); len(thread) > 0 {
		lines = append(lines, "", "Thread")
		lines = append(lines, thread...)
	} else if message := strings.TrimSpace(firstNonEmpty(item.Text, item.Detail, item.Title)); message != "" {
		lines = append(lines, "", "Message")
		lines = append(lines, chatBubbleLines(item, message, true)...)
	}
	if properties := chatPropertyLines(item); len(properties) > 0 {
		lines = append(lines, "", "Properties")
		lines = append(lines, properties...)
	}
	if ids := chatIDLines(item); len(ids) > 0 {
		lines = append(lines, "", "IDs")
		lines = append(lines, ids...)
	}
	if len(lines) == 0 {
		return []string{"No detail for this message."}
	}
	return lines
}

func documentDetailLines(item Item) []string {
	var lines []string
	title := firstNonEmpty(item.Title, item.ID, "Untitled")
	lines = append(lines, title)
	if meta := documentMetaLine(item); meta != "" {
		lines = append(lines, dim(meta))
	}
	if location := documentLocationLines(item); len(location) > 0 {
		lines = append(lines, "", "Location")
		lines = append(lines, location...)
	}
	preview := documentPreview(item)
	if preview != "" {
		lines = append(lines, "", "Preview")
		lines = append(lines, wrapLines(preview, 1000)...)
	}
	if metadata := documentPropertyLines(item); len(metadata) > 0 {
		lines = append(lines, "", "Properties")
		lines = append(lines, metadata...)
	}
	if len(lines) == 0 {
		return []string{"No detail for this document."}
	}
	return lines
}

func chatHeaderLine(item Item) string {
	parts := []string{
		firstNonEmpty(item.Container, item.Scope),
		itemAuthor(item),
		shortTimestamp(firstNonEmpty(item.CreatedAt, item.UpdatedAt)),
	}
	header := joinNonEmpty(parts, "  ")
	if header == "" {
		return firstNonEmpty(item.Title, item.ID)
	}
	return header
}

func chatMetaLine(item Item) string {
	parts := []string{
		fieldLine("id", item.ID),
		fieldLine("thread", threadKey(item)),
		fieldLine("kind", itemKind(item)),
	}
	return joinNonEmpty(parts, "  ")
}

func documentMetaLine(item Item) string {
	parts := []string{
		itemKind(item),
		firstNonEmpty(item.Container, item.Scope),
		shortTimestamp(firstNonEmpty(item.UpdatedAt, item.CreatedAt)),
	}
	return joinNonEmpty(parts, "  ")
}

func documentPreview(item Item) string {
	if text := strings.TrimSpace(item.Text); text != "" {
		return text
	}
	detail := strings.TrimSpace(item.Detail)
	if detail == "" || looksLikeFieldDump(detail) {
		return ""
	}
	return detail
}

func documentLocationLines(item Item) []string {
	return compactNonEmpty([]string{
		fieldLine("parent", item.ParentID),
		fieldLine("container", item.Container),
		fieldLine("workspace", item.Scope),
		fieldLine("url", item.URL),
	})
}

func documentPropertyLines(item Item) []string {
	lines := compactNonEmpty([]string{
		fieldLine("kind", itemKind(item)),
		fieldLine("source", item.Source),
		fieldLine("created", shortTimestamp(item.CreatedAt)),
		fieldLine("updated", shortTimestamp(item.UpdatedAt)),
		fieldLine("id", item.ID),
	})
	lines = append(lines, compactFieldLines(item.Fields, "source", "space_id", "collection_id", "parent_table")...)
	return lines
}

func looksLikeFieldDump(value string) bool {
	lines := compactNonEmpty(strings.Split(value, "\n"))
	if len(lines) == 0 {
		return false
	}
	fieldLines := 0
	for _, line := range lines {
		if strings.Contains(line, "=") || strings.HasPrefix(strings.TrimSpace(line), "url:") || strings.HasPrefix(strings.TrimSpace(line), "url=") {
			fieldLines++
		}
	}
	return fieldLines == len(lines)
}

func indentWrappedLines(value string, indent, width int) []string {
	prefix := strings.Repeat(" ", maxInt(0, indent))
	raw := wrapLines(value, width)
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		out = append(out, prefix+line)
	}
	return out
}

func detailContextLines(item Item, includeTitle bool) []string {
	var lines []string
	fields := []string{
		fieldLine("container", item.Container),
		fieldLine("author", item.Author),
		fieldLine("kind", itemKind(item)),
		fieldLine("source", item.Source),
		fieldLine("scope", item.Scope),
		fieldLine("created", shortTimestamp(item.CreatedAt)),
		fieldLine("updated", shortTimestamp(item.UpdatedAt)),
		fieldLine("id", item.ID),
		fieldLine("parent", item.ParentID),
		fieldLine("url", item.URL),
	}
	if includeTitle {
		fields = append(fields, fieldLine("title", item.Title))
	}
	for _, line := range fields {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(item.Tags) > 0 {
		lines = append(lines, "tags="+strings.Join(item.Tags, " "))
	}
	if len(item.Fields) > 0 {
		keys := make([]string, 0, len(item.Fields))
		for key := range item.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if line := fieldLine(key, item.Fields[key]); line != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

func (m model) threadLines(selected Item) []string {
	key := threadKey(selected)
	if key == "" {
		return nil
	}
	var lines []string
	for _, itemIndex := range m.currentGroupMembers() {
		if itemIndex < 0 || itemIndex >= len(m.items) {
			continue
		}
		item := m.items[itemIndex]
		if threadKey(item) != key {
			continue
		}
		text := firstNonEmpty(item.Text, item.Detail, item.Title)
		lines = append(lines, chatBubbleLines(item, text, item.ID == selected.ID)...)
	}
	if len(lines) <= 1 {
		return nil
	}
	return lines
}

func chatBubbleLines(item Item, text string, selected bool) []string {
	var lines []string
	prefix := "  "
	if selected {
		prefix = "> "
	}
	header := joinNonEmpty([]string{itemAuthor(item), shortTimestamp(firstNonEmpty(item.CreatedAt, item.UpdatedAt))}, "  ")
	if header != "" {
		lines = append(lines, prefix+header)
	}
	body := indentWrappedLines(text, lipgloss.Width(prefix)+2, 1000)
	if len(body) == 0 {
		body = []string{strings.Repeat(" ", lipgloss.Width(prefix)+2) + "(empty)"}
	}
	lines = append(lines, body...)
	return lines
}

func chatPropertyLines(item Item) []string {
	return compactNonEmpty([]string{
		fieldLine("channel", item.Container),
		fieldLine("scope", item.Scope),
		fieldLine("author", itemAuthor(item)),
		fieldLine("kind", itemKind(item)),
		fieldLine("source", item.Source),
		fieldLine("created", shortTimestamp(item.CreatedAt)),
		fieldLine("updated", shortTimestamp(item.UpdatedAt)),
		fieldLine("attachments", fieldValue(item, "attachments")),
		fieldLine("pinned", fieldValue(item, "pinned")),
		fieldLine("subtype", fieldValue(item, "subtype")),
	})
}

func chatIDLines(item Item) []string {
	lines := compactNonEmpty([]string{
		fieldLine("id", item.ID),
		fieldLine("thread", threadKey(item)),
		fieldLine("parent", item.ParentID),
	})
	lines = append(lines, compactFieldLines(item.Fields, "guild_id", "channel_id", "author_id", "user_id", "ts", "reply_to")...)
	return lines
}

func compactFieldLines(fields map[string]string, keys ...string) []string {
	if len(fields) == 0 {
		return nil
	}
	lines := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		value := fieldValue(Item{Fields: fields}, key)
		if line := fieldLine(key, value); line != "" {
			lines = append(lines, line)
		}
		seen[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	for key, value := range fields {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if _, ok := seen[normalized]; ok {
			continue
		}
		if line := fieldLine(key, value); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func threadKey(item Item) string {
	for _, value := range []string{
		fieldValue(item, "thread"),
		fieldValue(item, "reply_to"),
		strings.TrimSpace(item.ParentID),
		fieldValue(item, "ts"),
		strings.TrimSpace(item.ID),
	} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func rowListLine(item Item, width int) string {
	width = maxInt(width, 1)
	title := item.Title
	if item.Depth > 0 {
		title = strings.Repeat("  ", minInt(item.Depth, 6)) + "-> " + title
	}
	if width >= 24 && width < 68 {
		return compactRowListLine(item, title, width)
	}
	if width < 68 {
		return truncateCells(title, width)
	}
	kind := rowKind(item)
	when := rowWhen(item)
	age := rowAge(item)
	where := rowWhere(item)
	author := itemAuthor(item)
	kindW := minInt(maxInt(5, width/10), 10)
	whenW := minInt(maxInt(10, width/6), 16)
	ageW := minInt(maxInt(4, width/16), 7)
	whereW := minInt(maxInt(10, width/5), 22)
	authorW := minInt(maxInt(8, width/7), 18)
	titleW := maxInt(1, width-kindW-whenW-ageW-whereW-authorW-5)
	return padCells(truncateCells(kind, kindW), kindW) + " " +
		padCells(truncateCells(when, whenW), whenW) + " " +
		padCells(truncateCells(age, ageW), ageW) + " " +
		padCells(truncateCells(where, whereW), whereW) + " " +
		padCells(truncateCells(author, authorW), authorW) + " " +
		truncateCells(title, titleW)
}

func compactRowListLine(item Item, title string, width int) string {
	if width < 34 {
		whenW := 5
		titleW := maxInt(1, width-whenW-1)
		return padCells(truncateCells(compactDate(item), whenW), whenW) + " " +
			truncateCells(title, titleW)
	}
	whenW := 5
	ageW := 4
	authorW := minInt(maxInt(5, width/6), 9)
	titleW := maxInt(1, width-whenW-ageW-authorW-3)
	return padCells(truncateCells(compactDate(item), whenW), whenW) + " " +
		padCells(truncateCells(rowAge(item), ageW), ageW) + " " +
		padCells(truncateCells(itemAuthor(item), authorW), authorW) + " " +
		truncateCells(title, titleW)
}

func groupListLine(group itemGroup, width int) string {
	width = maxInt(width, 1)
	if width >= 24 && width < 68 {
		return compactGroupListLine(group, width)
	}
	if width < 68 {
		return truncateCells(group.Title, width)
	}
	kindW := minInt(maxInt(6, width/8), 10)
	countW := minInt(maxInt(4, width/12), 7)
	timeW := minInt(maxInt(12, width/5), 18)
	ageW := minInt(maxInt(4, width/16), 7)
	scopeW := minInt(maxInt(8, width/7), 16)
	titleW := maxInt(1, width-kindW-countW-timeW-ageW-scopeW-5)
	return padCells(truncateCells(group.Kind, kindW), kindW) + " " +
		padCells(fmt.Sprintf("%d", group.Count), countW) + " " +
		padCells(truncateCells(shortTimestamp(group.Latest), timeW), timeW) + " " +
		padCells(truncateCells(ageFromTimestamp(group.Latest), ageW), ageW) + " " +
		padCells(truncateCells(group.Scope, scopeW), scopeW) + " " +
		truncateCells(group.Title, titleW)
}

func compactGroupListLine(group itemGroup, width int) string {
	countW := 3
	ageW := 4
	if width >= 44 {
		kindW := 8
		titleW := maxInt(1, width-kindW-countW-ageW-3)
		return padCells(truncateCells(group.Kind, kindW), kindW) + " " +
			padCells(fmt.Sprintf("%d", group.Count), countW) + " " +
			padCells(truncateCells(ageFromTimestamp(group.Latest), ageW), ageW) + " " +
			truncateCells(group.Title, titleW)
	}
	titleW := maxInt(1, width-countW-ageW-2)
	return padCells(fmt.Sprintf("%d", group.Count), countW) + " " +
		padCells(truncateCells(ageFromTimestamp(group.Latest), ageW), ageW) + " " +
		truncateCells(group.Title, titleW)
}

func groupListHeader(width int, active sortMode) string {
	width = maxInt(width, 1)
	if width >= 24 && width < 68 {
		return tagStyle(width).Bold(true).Render(compactGroupListHeader(width, active))
	}
	if width < 68 {
		return tagStyle(width).Render(padCells("GROUP", width))
	}
	kindW := minInt(maxInt(6, width/8), 10)
	countW := minInt(maxInt(4, width/12), 7)
	timeW := minInt(maxInt(12, width/5), 18)
	ageW := minInt(maxInt(4, width/16), 7)
	scopeW := minInt(maxInt(8, width/7), 16)
	titleW := maxInt(1, width-kindW-countW-timeW-ageW-scopeW-5)
	kind := "TYPE"
	count := "COUNT"
	when := "LATEST"
	age := "AGE"
	scope := "SCOPE"
	title := "GROUP"
	switch active {
	case sortKind:
		kind = "TYPE v"
	case sortNewest, sortOldest:
		when = "LATEST v"
	case sortScope:
		scope = "SCOPE v"
	case sortTitle, sortContainer, sortAuthor:
		title = "GROUP v"
	}
	line := padCells(truncateCells(kind, kindW), kindW) + " " +
		padCells(truncateCells(count, countW), countW) + " " +
		padCells(truncateCells(when, timeW), timeW) + " " +
		padCells(truncateCells(age, ageW), ageW) + " " +
		padCells(truncateCells(scope, scopeW), scopeW) + " " +
		truncateCells(title, titleW)
	return tagStyle(width).Bold(true).Render(line)
}

func compactGroupListHeader(width int, active sortMode) string {
	count := "N"
	age := "AGE"
	title := "GROUP"
	if active == sortNewest || active == sortOldest {
		age = "AGE v"
	}
	if active == sortTitle || active == sortContainer || active == sortAuthor {
		title = "GROUP v"
	}
	countW := 3
	ageW := 4
	if width >= 44 {
		kindW := 8
		kind := "TYPE"
		if active == sortKind {
			kind = "TYPE v"
		}
		titleW := maxInt(1, width-kindW-countW-ageW-3)
		return padCells(truncateCells(kind, kindW), kindW) + " " +
			padCells(truncateCells(count, countW), countW) + " " +
			padCells(truncateCells(age, ageW), ageW) + " " +
			truncateCells(title, titleW)
	}
	titleW := maxInt(1, width-countW-ageW-2)
	return padCells(truncateCells(count, countW), countW) + " " +
		padCells(truncateCells(age, ageW), ageW) + " " +
		truncateCells(title, titleW)
}

func rowListHeader(width int, active sortMode) string {
	width = maxInt(width, 1)
	if width >= 24 && width < 68 {
		return tagStyle(width).Bold(true).Render(compactRowListHeader(width, active))
	}
	if width < 68 {
		return tagStyle(width).Render(padCells("TITLE", width))
	}
	kindW := minInt(maxInt(5, width/10), 10)
	whenW := minInt(maxInt(10, width/6), 16)
	ageW := minInt(maxInt(4, width/16), 7)
	whereW := minInt(maxInt(10, width/5), 22)
	authorW := minInt(maxInt(8, width/7), 18)
	titleW := maxInt(1, width-kindW-whenW-ageW-whereW-authorW-5)
	kind := "KIND"
	when := "TIME"
	age := "AGE"
	where := "WHERE"
	author := "AUTHOR"
	title := "TITLE"
	switch active {
	case sortKind:
		kind = "KIND v"
	case sortScope, sortContainer, sortAuthor, sortNewest, sortOldest:
		if active == sortAuthor {
			author = "AUTHOR v"
		} else if active == sortScope || active == sortContainer {
			where = "WHERE v"
		} else {
			when = "WHEN v"
		}
	case sortTitle:
		title = "TITLE v"
	}
	line := padCells(truncateCells(kind, kindW), kindW) + " " +
		padCells(truncateCells(when, whenW), whenW) + " " +
		padCells(truncateCells(age, ageW), ageW) + " " +
		padCells(truncateCells(where, whereW), whereW) + " " +
		padCells(truncateCells(author, authorW), authorW) + " " +
		truncateCells(title, titleW)
	return tagStyle(width).Bold(true).Render(line)
}

func compactRowListHeader(width int, active sortMode) string {
	if width < 34 {
		whenW := 5
		titleW := maxInt(1, width-whenW-1)
		return padCells(truncateCells("DATE", whenW), whenW) + " " + truncateCells("TITLE", titleW)
	}
	timeLabel := "DATE"
	age := "AGE"
	author := "WHO"
	title := "TITLE"
	switch active {
	case sortNewest, sortOldest:
		age = "AGE v"
	case sortAuthor:
		author = "WHO v"
	case sortTitle:
		title = "TITLE v"
	}
	whenW := 5
	ageW := 4
	authorW := minInt(maxInt(5, width/6), 9)
	titleW := maxInt(1, width-whenW-ageW-authorW-3)
	return padCells(truncateCells(timeLabel, whenW), whenW) + " " +
		padCells(truncateCells(age, ageW), ageW) + " " +
		padCells(truncateCells(author, authorW), authorW) + " " +
		truncateCells(title, titleW)
}

func (m *model) sortRowsFromHeader(x int) {
	width := paneContentWidth(m.layout().rows.w)
	if width >= 34 && width < 68 {
		m.sortCompactHeader(x, width)
		return
	}
	if width < 68 {
		m.setSortMode(sortTitle)
		return
	}
	kindW := minInt(maxInt(5, width/10), 10)
	whenW := minInt(maxInt(10, width/6), 16)
	ageW := minInt(maxInt(4, width/16), 7)
	whereW := minInt(maxInt(10, width/5), 22)
	authorW := minInt(maxInt(8, width/7), 18)
	switch {
	case x < kindW:
		m.setSortMode(sortKind)
	case x < kindW+1+whenW:
		if m.sortMode == sortNewest {
			m.setSortMode(sortOldest)
		} else {
			m.setSortMode(sortNewest)
		}
	case x < kindW+1+whenW+1+ageW:
		if m.sortMode == sortNewest {
			m.setSortMode(sortOldest)
		} else {
			m.setSortMode(sortNewest)
		}
	case x < kindW+1+whenW+1+ageW+1+whereW:
		m.setSortMode(sortContainer)
	case x < kindW+1+whenW+1+ageW+1+whereW+1+authorW:
		m.setSortMode(sortAuthor)
	default:
		m.setSortMode(sortTitle)
	}
}

func (m *model) sortCompactHeader(x int, width int) {
	whenW := 5
	ageW := 4
	authorW := minInt(maxInt(5, width/6), 9)
	switch {
	case x < whenW+1+ageW:
		if m.sortMode == sortNewest {
			m.setSortMode(sortOldest)
		} else {
			m.setSortMode(sortNewest)
		}
	case x < whenW+1+ageW+1+authorW:
		m.setSortMode(sortAuthor)
	default:
		m.setSortMode(sortTitle)
	}
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

func rowAge(item Item) string {
	if t, ok := itemSortTime(item); ok {
		return compactAge(time.Since(t))
	}
	return ""
}

func compactDate(item Item) string {
	if t, ok := itemSortTime(item); ok {
		return t.UTC().Format("01-02")
	}
	return ""
}

func ageFromTimestamp(value string) string {
	t, ok := parseTimestamp(value)
	if !ok {
		return ""
	}
	return compactAge(time.Since(t))
}

func compactAge(duration time.Duration) string {
	if duration < 0 {
		duration = -duration
	}
	switch {
	case duration < time.Minute:
		return "now"
	case duration < time.Hour:
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	case duration < 48*time.Hour:
		return fmt.Sprintf("%dh", int(duration/time.Hour))
	case duration < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(duration/(24*time.Hour)))
	case duration < 730*24*time.Hour:
		return fmt.Sprintf("%dmo", int(duration/(30*24*time.Hour)))
	default:
		return fmt.Sprintf("%dy", int(duration/(365*24*time.Hour)))
	}
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
		Background(lipgloss.Color("#0d1321")).
		Width(width)
}

func bold(value string) string {
	return lipgloss.NewStyle().Bold(true).Render(value)
}

func dim(value string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(archiveMutedFG)).Render(value)
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

func floatingMenuStyle(width, height int, palette actionMenuPalette) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(maxInt(1, width-2)).
		Height(maxInt(1, height-2)).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(palette.accent)).
		Background(lipgloss.Color(palette.background)).
		Foreground(lipgloss.Color(palette.foreground))
}

func selectedMenuLineStyle(width int, palette actionMenuPalette) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(maxInt(1, width)).
		Background(lipgloss.Color(palette.selectedBG)).
		Foreground(lipgloss.Color(palette.selectedFG)).
		Bold(true)
}

func separator(width int) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(archiveBorderColor)).
		Width(width).
		Render(strings.Repeat("-", minInt(width, 120)))
}

func overlayBlock(base, block string, x, y, width int) string {
	baseLines := strings.Split(base, "\n")
	blockLines := strings.Split(block, "\n")
	for offset, line := range blockLines {
		row := y + offset
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLine := baseLines[row]
		prefix := strings.Repeat(" ", maxInt(0, x))
		if x > 0 && baseLine != "" {
			prefix = padCells(ansi.Cut(baseLine, 0, x), x)
		}
		lineWidth := ansi.StringWidth(line)
		suffixStart := maxInt(0, x+lineWidth)
		suffix := ""
		if suffixStart < ansi.StringWidth(baseLine) {
			suffix = ansi.Cut(baseLine, suffixStart, width)
		}
		rendered := prefix + line + suffix
		if width > 0 {
			rendered = truncateCells(rendered, width)
		}
		baseLines[row] = rendered
	}
	return strings.Join(baseLines, "\n")
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
