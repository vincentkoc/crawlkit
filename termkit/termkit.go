package termkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

var ErrNotTerminal = errors.New("terminal UI requires an interactive terminal")

type Item struct {
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type Options struct {
	Title        string
	EmptyMessage string
	Items        []Item
	Stdin        io.Reader
	Stdout       io.Writer
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

type model struct {
	title       string
	items       []Item
	filtered    []int
	selected    int
	offset      int
	width       int
	height      int
	query       string
	filterMode  bool
	showDetails bool
}

func newModel(opts Options) model {
	m := model{
		title:       strings.TrimSpace(opts.Title),
		items:       append([]Item(nil), opts.Items...),
		width:       100,
		height:      30,
		showDetails: true,
	}
	if m.title == "" {
		m.title = "archive"
	}
	m.applyFilter()
	return m
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
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "pgup", "ctrl+b":
			m.move(-m.pageSize())
		case "pgdown", "ctrl+f":
			m.move(m.pageSize())
		case "home", "g":
			m.selected = 0
			m.ensureVisible()
		case "end", "G":
			m.selected = len(m.filtered) - 1
			m.ensureVisible()
		case "/", "f":
			m.filterMode = true
		case "esc":
			if m.query != "" {
				m.query = ""
				m.applyFilter()
			}
		case "enter", " ":
			m.showDetails = !m.showDetails
		}
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	width := maxInt(m.width, 40)
	b.WriteString(ansiBold)
	b.WriteString(truncate(m.title, width))
	b.WriteString(ansiReset)
	b.WriteByte('\n')
	b.WriteString(ansiDim)
	b.WriteString(fmt.Sprintf("%d rows", len(m.filtered)))
	if m.query != "" {
		b.WriteString(" filtered by ")
		b.WriteString(strconvQuote(m.query))
	}
	b.WriteString("  j/k move  / filter  enter details  q quit")
	b.WriteString(ansiReset)
	b.WriteByte('\n')
	if m.filterMode {
		b.WriteString(ansiCyan)
		b.WriteString("filter> ")
		b.WriteString(ansiReset)
		b.WriteString(m.query)
		b.WriteByte('\n')
	}
	b.WriteString(strings.Repeat("-", minInt(width, 120)))
	b.WriteByte('\n')
	if len(m.filtered) == 0 {
		b.WriteString("no rows match")
		return b.String()
	}
	rows := m.visibleRows()
	for _, index := range rows {
		item := m.items[m.filtered[index]]
		selected := index == m.selected
		if selected {
			b.WriteString(ansiReverse)
			b.WriteString("> ")
		} else {
			b.WriteString("  ")
		}
		line := item.Title
		if item.Subtitle != "" {
			line += "  " + ansiDim + item.Subtitle + ansiReset
			if selected {
				line += ansiReverse
			}
		}
		b.WriteString(truncate(line, width))
		if selected {
			b.WriteString(ansiReset)
		}
		b.WriteByte('\n')
	}
	if m.showDetails {
		b.WriteString(strings.Repeat("-", minInt(width, 120)))
		b.WriteByte('\n')
		item := m.items[m.filtered[m.selected]]
		if len(item.Tags) > 0 {
			b.WriteString(ansiCyan)
			b.WriteString(strings.Join(item.Tags, "  "))
			b.WriteString(ansiReset)
			b.WriteByte('\n')
		}
		detail := strings.TrimSpace(item.Detail)
		if detail == "" {
			detail = item.Subtitle
		}
		b.WriteString(wrap(detail, width))
	}
	return b.String()
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
	reserved := 7
	if m.filterMode {
		reserved++
	}
	if !m.showDetails {
		reserved -= 3
	}
	return maxInt(3, m.height-reserved)
}

func (m model) visibleRows() []int {
	end := minInt(len(m.filtered), m.offset+m.pageSize())
	out := make([]int, 0, end-m.offset)
	for i := m.offset; i < end; i++ {
		out = append(out, i)
	}
	return out
}

func truncate(value string, width int) string {
	if width <= 1 || len(value) <= width {
		return value
	}
	return value[:maxInt(width-3, 0)] + "..."
}

func wrap(value string, width int) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	if width <= 0 || len(value) <= width {
		return value
	}
	var b strings.Builder
	for len(value) > width {
		cut := strings.LastIndex(value[:width], " ")
		if cut <= 0 {
			cut = width
		}
		b.WriteString(value[:cut])
		b.WriteByte('\n')
		value = strings.TrimSpace(value[cut:])
	}
	b.WriteString(value)
	return b.String()
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

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiCyan    = "\033[36m"
	ansiReverse = "\033[7m"
)
