package main

import (
	"fmt"
	"os"
	"sort"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type appState int

const (
	stateScanning appState = iota
	stateSelecting
	stateDeleting
	stateDone
)

// listItem is a discovered folder plus its checkbox state.
type listItem struct {
	target
	checked bool
}

// deleteResult records the outcome of removing one folder.
type deleteResult struct {
	path string
	size int64
	err  error
}

// --- messages ---

type scanDoneMsg struct{ targets []target }

type deleteDoneMsg deleteResult

// --- styles ---

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	checkedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	sizeStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	footerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	selectedStyle = lipgloss.NewStyle().Bold(true)
)

type model struct {
	opts    options
	state   appState
	spinner spinner.Model
	scanned *atomic.Int64

	items      []listItem
	cursor     int
	offset     int // scroll offset into items
	sortBySize bool

	// deletion progress
	queue   []listItem
	delIdx  int
	results []deleteResult
	freed   int64

	width  int
	height int

	aborted bool
}

func newModel(opts options) model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = cursorStyle
	return model{
		opts:       opts,
		state:      stateScanning,
		spinner:    sp,
		scanned:    &atomic.Int64{},
		sortBySize: true, // biggest space hogs first by default
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.scanCmd())
}

// scanCmd runs the (blocking) filesystem scan off the UI loop.
func (m model) scanCmd() tea.Cmd {
	opts, scanned := m.opts, m.scanned
	return func() tea.Msg {
		return scanDoneMsg{targets: collectTargets(opts, scanned)}
	}
}

// deleteCmd removes a single folder and reports the result.
func deleteCmd(it listItem) tea.Cmd {
	return func() tea.Msg {
		err := os.RemoveAll(it.path)
		return deleteDoneMsg{path: it.path, size: it.size, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		if m.state == stateScanning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case scanDoneMsg:
		m.applySort(msg.targets)
		if len(m.items) == 0 {
			m.state = stateDone
			return m, nil
		}
		m.state = stateSelecting
		return m, nil

	case deleteDoneMsg:
		res := deleteResult(msg)
		m.results = append(m.results, res)
		if res.err == nil {
			m.freed += res.size
		}
		m.delIdx++
		if m.delIdx < len(m.queue) {
			return m, deleteCmd(m.queue[m.delIdx])
		}
		m.state = stateDone
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateSelecting:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.aborted = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.clampOffset()
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
			m.clampOffset()
		case " ":
			m.items[m.cursor].checked = !m.items[m.cursor].checked
		case "a":
			m.toggleAll()
		case "s":
			m.sortBySize = !m.sortBySize
			m.resort()
		case "enter":
			m.queue = m.selectedItems()
			if len(m.queue) == 0 {
				return m, nil // nothing selected; stay put
			}
			m.state = stateDeleting
			m.delIdx = 0
			return m, deleteCmd(m.queue[0])
		}
		return m, nil

	case stateDone:
		// any key exits
		return m, tea.Quit
	}

	// During scanning / deleting, allow ctrl+c to bail out.
	if msg.String() == "ctrl+c" {
		m.aborted = true
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	switch m.state {
	case stateScanning:
		return fmt.Sprintf("\n %s Scanning... %d directories checked\n",
			m.spinner.View(), m.scanned.Load())

	case stateSelecting:
		return m.selectView()

	case stateDeleting:
		return m.deleteView()

	case stateDone:
		return m.doneView()
	}
	return ""
}

// --- selecting view ---

func (m model) visibleRows() int {
	// Reserve lines for title (2) and footer (2).
	rows := m.height - 4
	if rows < 1 {
		rows = 10
	}
	return rows
}

func (m *model) clampOffset() {
	rows := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
}

func (m model) selectView() string {
	var b []byte
	selCount, selSize := m.selection()
	title := titleStyle.Render(fmt.Sprintf("Select node_modules to delete  —  %d/%d selected, %s",
		selCount, len(m.items), humanSize(selSize)))
	b = append(b, "\n "...)
	b = append(b, title...)
	b = append(b, "\n\n"...)

	rows := m.visibleRows()
	end := m.offset + rows
	if end > len(m.items) {
		end = len(m.items)
	}
	for i := m.offset; i < end; i++ {
		it := m.items[i]
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("▌ ")
		}
		box := "[ ]"
		if it.checked {
			box = checkedStyle.Render("[x]")
		}
		line := fmt.Sprintf("%s %s  %s", box, it.path, sizeStyle.Render("("+humanSize(it.size)+")"))
		if i == m.cursor {
			line = selectedStyle.Render(line)
		}
		b = append(b, " "...)
		b = append(b, cursor...)
		b = append(b, line...)
		b = append(b, '\n')
	}

	sortLabel := "path"
	if m.sortBySize {
		sortLabel = "size"
	}
	footer := footerStyle.Render(fmt.Sprintf(
		"\n ↑/↓ move • space toggle • a all/none • s sort (%s) • enter delete • q quit", sortLabel))
	b = append(b, footer...)
	b = append(b, '\n')
	return string(b)
}

// --- deleting view ---

func (m model) deleteView() string {
	var b []byte
	b = append(b, "\n "...)
	b = append(b, titleStyle.Render(fmt.Sprintf("Deleting... %d/%d", m.delIdx, len(m.queue)))...)
	b = append(b, "\n\n"...)
	// Show the most recent results (tail), bounded to the viewport.
	start := 0
	if rows := m.visibleRows(); len(m.results) > rows {
		start = len(m.results) - rows
	}
	for _, r := range m.results[start:] {
		if r.err != nil {
			b = append(b, " "...)
			b = append(b, errStyle.Render(fmt.Sprintf("✗ failed %s: %v", r.path, r.err))...)
		} else {
			b = append(b, " "...)
			b = append(b, okStyle.Render(fmt.Sprintf("✓ deleted %s (%s)", r.path, humanSize(r.size)))...)
		}
		b = append(b, '\n')
	}
	b = append(b, footerStyle.Render(fmt.Sprintf("\n freed ~%s so far", humanSize(m.freed)))...)
	b = append(b, '\n')
	return string(b)
}

// --- done view ---

func (m model) doneView() string {
	if len(m.items) == 0 {
		return "\n Nothing to clean. No node_modules folders found.\n\n Press any key to exit.\n"
	}
	if len(m.results) == 0 {
		return "\n Aborted. Nothing was deleted.\n\n Press any key to exit.\n"
	}
	var deleted, failed int
	for _, r := range m.results {
		if r.err == nil {
			deleted++
		} else {
			failed++
		}
	}
	var b []byte
	b = append(b, "\n "...)
	b = append(b, okStyle.Render(fmt.Sprintf("Done. Removed %d folder(s), freed ~%s.", deleted, humanSize(m.freed)))...)
	b = append(b, '\n')
	if failed > 0 {
		b = append(b, " "...)
		b = append(b, errStyle.Render(fmt.Sprintf("%d folder(s) failed to delete.", failed))...)
		b = append(b, '\n')
	}
	b = append(b, footerStyle.Render("\n Press any key to exit.")...)
	b = append(b, '\n')
	return string(b)
}

// --- selection helpers ---

func (m model) selection() (count int, size int64) {
	for _, it := range m.items {
		if it.checked {
			count++
			size += it.size
		}
	}
	return
}

func (m model) selectedItems() []listItem {
	var out []listItem
	for _, it := range m.items {
		if it.checked {
			out = append(out, it)
		}
	}
	return out
}

func (m *model) toggleAll() {
	count, _ := m.selection()
	all := count == len(m.items)
	for i := range m.items {
		m.items[i].checked = !all
	}
}

// applySort builds the item list from raw targets in the current sort order.
func (m *model) applySort(targets []target) {
	m.items = make([]listItem, len(targets))
	for i, t := range targets {
		m.items[i] = listItem{target: t}
	}
	m.resort()
}

// resort reorders the existing items, preserving checkbox state.
func (m *model) resort() {
	if m.sortBySize {
		sort.SliceStable(m.items, func(i, j int) bool {
			return m.items[i].size > m.items[j].size
		})
	} else {
		sort.SliceStable(m.items, func(i, j int) bool {
			return m.items[i].path < m.items[j].path
		})
	}
	if m.cursor >= len(m.items) {
		m.cursor = 0
	}
	m.offset = 0
}

// runTUI launches the interactive selector and prints a persistent summary
// after it exits (the alt-screen contents disappear on quit).
func runTUI(opts options) {
	p := tea.NewProgram(newModel(opts), tea.WithAltScreen())
	res, err := p.Run()
	if err != nil {
		// Could not start the TUI — fall back to the plain path.
		fmt.Fprintf(os.Stderr, "TUI failed to start (%v); falling back to text mode.\n", err)
		runPlain(opts)
		return
	}

	final, ok := res.(model)
	if !ok {
		return
	}
	printSummary(final)
}

func printSummary(m model) {
	if len(m.items) == 0 {
		fmt.Println("Nothing to clean. No node_modules folders found.")
		return
	}
	if len(m.results) == 0 {
		fmt.Println("Aborted. Nothing was deleted.")
		return
	}
	var deleted, failed int
	for _, r := range m.results {
		if r.err == nil {
			deleted++
		} else {
			failed++
			fmt.Fprintf(os.Stderr, "failed: %s -> %v\n", r.path, r.err)
		}
	}
	fmt.Printf("Done: removed %d folder(s), freed ~%s.\n", deleted, humanSize(m.freed))
	if failed > 0 {
		fmt.Printf("%d folder(s) failed to delete.\n", failed)
	}
}
