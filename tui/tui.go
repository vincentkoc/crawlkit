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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-isatty"
)

var ErrNotTerminal = errors.New("terminal UI requires an interactive terminal")

type Item struct {
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Depth    int      `json:"depth,omitempty"`
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
		Title:    title,
		Subtitle: r.subtitleForLayout(layout),
		Detail:   detail,
		Tags:     tags,
		Depth:    depth,
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
	sourceKind     string
	sourceLocation string
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
	case tea.MouseMsg:
		if typed.Type == tea.MouseWheelUp {
			m.move(-3)
		} else if typed.Type == tea.MouseWheelDown {
			m.move(3)
		}
	case tea.KeyMsg:
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
			}
		case "down", "j":
			if m.focus == focusRows {
				m.move(1)
			}
		case "pgup", "ctrl+b":
			if m.focus == focusRows {
				m.move(-m.pageSize())
			}
		case "pgdown", "ctrl+f":
			if m.focus == focusRows {
				m.move(m.pageSize())
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
			m.filterMode = true
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
	line := m.title + "  " + status
	if m.filterMode {
		line += "  filter> " + m.query
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
			line := item.Title
			if item.Depth > 0 {
				line = strings.Repeat("  ", minInt(item.Depth, 6)) + "-> " + line
			}
			line = truncateCells(prefix+line, paneContentWidth(rect.w))
			lines = append(lines, rowStyle(paneContentWidth(rect.w), selected && m.focus == focusRows).Render(line))
		}
	}
	return pane("Rows", m.positionLabel(), lines, rect, m.focus == focusRows, "#5bc0eb")
}

func (m model) renderContextPane(rect rect) string {
	item, ok := m.selectedItem()
	if !ok {
		return pane("Context", "", []string{"No row selected."}, rect, m.focus == focusContext, "#9bc53d")
	}
	lines := []string{
		fieldLine("title", truncateCells(item.Title, maxInt(1, paneContentWidth(rect.w)-6))),
		fieldLine("subtitle", item.Subtitle),
	}
	if len(item.Tags) > 0 {
		lines = append(lines, "tags="+strings.Join(item.Tags, " "))
	}
	lines = compactNonEmpty(lines)
	return pane("Context", paneFocusLabel(m.focus == focusContext), lines, rect, m.focus == focusContext, "#9bc53d")
}

func (m model) renderDetailPane(rect rect) string {
	item, ok := m.selectedItem()
	if !ok {
		return pane("Detail", "", []string{"No row selected."}, rect, m.focus == focusDetail, "#f2c14e")
	}
	detail := strings.TrimSpace(item.Detail)
	if detail == "" {
		detail = item.Subtitle
	}
	lines := wrapLines(detail, paneContentWidth(rect.w))
	if len(lines) == 0 {
		lines = []string{"No detail for this row."}
	}
	return pane("Detail", paneFocusLabel(m.focus == focusDetail), lines, rect, m.focus == focusDetail, "#f2c14e")
}

func (m model) renderFooter(width int) string {
	line := "Ready"
	if m.filterMode {
		line = "Filtering"
	}
	if location := m.footerLocation(); location != "" {
		line += "  " + location
	}
	controls := "Tab panes  j/k move  / filter  enter details  q quit"
	bg, fg := footerPalette(m.sourceKind)
	statusLine := padCells(" "+truncateCells(line, maxInt(1, width-2)), width)
	controlsLine := padCells(" "+truncateCells(controls, maxInt(1, width-2)), width)
	return lipgloss.NewStyle().Width(width).Height(2).Background(bg).Foreground(fg).Render(statusLine + "\n" + controlsLine)
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
	m.ensureVisible()
}

func (m *model) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.query))
	m.filtered = m.filtered[:0]
	for i, item := range m.items {
		if query == "" || strings.Contains(strings.ToLower(item.Title+" "+item.Subtitle+" "+item.Detail+" "+strings.Join(item.Tags, " ")), query) {
			m.filtered = append(m.filtered, i)
		}
	}
	if len(m.filtered) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	m.selected = clampInt(m.selected, 0, len(m.filtered)-1)
	m.ensureVisible()
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
			rows:    rect{w: rowsW, h: bodyH},
			context: rect{w: rightW, h: contextH},
			detail:  rect{w: rightW, h: bodyH - contextH},
		}
	}
	rowsH := maxInt(5, bodyH*42/100)
	contextH := minInt(maxInt(4, bodyH*24/100), maxInt(3, bodyH-rowsH-3))
	return archiveLayout{
		rows:    rect{w: width, h: rowsH},
		context: rect{w: width, h: contextH},
		detail:  rect{w: width, h: bodyH - rowsH - contextH},
		stacked: true,
	}
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
	width := maxInt(rect.w, 12)
	height := maxInt(rect.h, 3)
	contentW := paneContentWidth(width)
	contentH := paneContentHeight(height)
	borderColor := "#475569"
	if focused {
		borderColor = accent
	}
	titleLine := title
	if strings.TrimSpace(subtitle) != "" {
		titleLine += "  " + subtitle
	}
	top := "+" + strings.Repeat("-", maxInt(0, width-2)) + "+"
	border := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#dfe7ef")).Bold(focused)
	header := border.Render("|") +
		headerStyle.Render(padCells(" "+truncateCells(titleLine, maxInt(1, contentW-1)), contentW)) +
		border.Render("|")
	body := make([]string, 0, contentH)
	for _, line := range lines {
		for _, wrapped := range wrapLines(line, contentW) {
			body = append(body, wrapped)
		}
	}
	if len(body) == 0 {
		body = append(body, "")
	}
	for len(body) < contentH {
		body = append(body, "")
	}
	if len(body) > contentH {
		body = body[:contentH]
	}
	out := []string{border.Render(top), header}
	for _, line := range body[:maxInt(0, contentH-1)] {
		out = append(out, border.Render("|")+padCells(truncateCells(line, contentW), contentW)+border.Render("|"))
	}
	out = append(out, border.Render(top))
	return strings.Join(out, "\n")
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

func titleStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#f8fafc")).
		Background(lipgloss.Color("#172033")).
		Width(width)
}

func mutedStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8f9aaa")).
		Width(width)
}

func accentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#7fb4d8"))
}

func tagStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7fb4d8")).
		Width(width)
}

func rowStyle(width int, selected bool) lipgloss.Style {
	style := lipgloss.NewStyle().Width(width)
	if selected {
		return style.
			Foreground(lipgloss.Color("#f8fafc")).
			Background(lipgloss.Color("#2f3f56"))
	}
	return style.Foreground(lipgloss.Color("#d7dee8"))
}

func separator(width int) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#475569")).
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
		return lipgloss.Color("#f2c14e"), lipgloss.Color("#05070d")
	default:
		return lipgloss.Color("#5bc0eb"), lipgloss.Color("#05070d")
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
