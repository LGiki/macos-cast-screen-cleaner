package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// ---------------------------------------------------------------------------
// States
// ---------------------------------------------------------------------------

type appState int

const (
	stateScanning   appState = iota
	stateEmpty               // nothing found
	stateSelecting           // interactive item selection
	stateConfirming          // confirm before removal
	stateSudo                // waiting for sudo prompt
	stateDone                // show results
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

type scanDoneMsg []CleanItem
type sudoDoneMsg struct{ err error }

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170"))

	accentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170"))

	catStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))

	checkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82"))

	uncheckStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	sudoBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("242"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("82"))

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	cleanStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82")).
			Bold(true)

	loadedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type model struct {
	state         appState
	items         []CleanItem
	cursor        int
	selected      map[int]bool
	spinner       spinner.Model
	results       []RemoveResult
	width         int
	height        int
	offset           int         // viewport scroll offset (first visible item index)
	confirmOffset    int         // scroll offset for confirm view
	doneOffset       int         // scroll offset for done view
	pendingUserItems []CleanItem // user-level items deferred until sudo completes
}

func newModel() model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("170"))
	return model{
		state:    stateScanning,
		selected: make(map[int]bool),
		spinner:  s,
		width:    80,
	}
}

// ---------------------------------------------------------------------------
// Bubbletea interface
// ---------------------------------------------------------------------------

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg { return scanDoneMsg(Scan()) },
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case scanDoneMsg:
		m.items = []CleanItem(msg)
		if len(m.items) == 0 {
			m.state = stateEmpty
		} else {
			m.state = stateSelecting
			for i := range m.items {
				m.selected[i] = true
			}
		}
		return m, nil

	case sudoDoneMsg:
		var sudoItems []CleanItem
		for i, item := range m.items {
			if m.selected[i] && item.NeedsSudo {
				sudoItems = append(sudoItems, item)
			}
		}
		if msg.err != nil {
			// sudo was cancelled or failed — mark all sudo items as failed
			for _, item := range sudoItems {
				m.results = append(m.results, RemoveResult{
					Item:  item,
					Error: fmt.Sprintf("sudo failed: %v", msg.err),
				})
			}
		} else {
			m.results = append(m.results, VerifySudoRemoval(sudoItems)...)
		}
		// Now remove user-level items that were deferred until sudo completed
		if len(m.pendingUserItems) > 0 {
			m.results = append(m.results, RemoveUserItems(m.pendingUserItems)...)
			m.pendingUserItems = nil
		}
		m.state = stateDone
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) View() string {
	switch m.state {
	case stateScanning:
		return m.viewScanning()
	case stateEmpty:
		return m.viewEmpty()
	case stateSelecting:
		return m.viewSelecting()
	case stateConfirming:
		return m.viewConfirming()
	case stateSudo:
		return ""
	case stateDone:
		return m.viewDone()
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Key handling
// ---------------------------------------------------------------------------

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.state {
	case stateEmpty:
		if key == "q" || key == "enter" || key == "esc" {
			return m, tea.Quit
		}

	case stateDone:
		switch key {
		case "q", "enter", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.doneOffset > 0 {
				m.doneOffset--
			}
		case "down", "j":
			if m.doneOffset < len(m.results)-1 {
				m.doneOffset++
			}
		}

	case stateSelecting:
		switch key {
		case "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.adjustViewport()
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				m.adjustViewport()
			}
		case " ":
			m.selected[m.cursor] = !m.selected[m.cursor]
		case "a":
			for i := range m.items {
				m.selected[i] = true
			}
		case "n":
			for i := range m.items {
				m.selected[i] = false
			}
		case "enter":
			if m.selectedCount() > 0 {
				m.state = stateConfirming
			}
		}

	case stateConfirming:
		switch key {
		case "y", "Y":
			return m.startRemoval()
		case "n", "N", "esc":
			m.state = stateSelecting
			m.confirmOffset = 0
		case "q":
			return m, tea.Quit
		case "up", "k":
			if m.confirmOffset > 0 {
				m.confirmOffset--
			}
		case "down", "j":
			if m.confirmOffset < m.selectedCount()-1 {
				m.confirmOffset++
			}
		}
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Removal flow
// ---------------------------------------------------------------------------

func (m model) startRemoval() (tea.Model, tea.Cmd) {
	var userItems, sudoItems []CleanItem
	for i, item := range m.items {
		if !m.selected[i] {
			continue
		}
		if item.NeedsSudo {
			sudoItems = append(sudoItems, item)
		} else {
			userItems = append(userItems, item)
		}
	}

	// If sudo items exist, suspend TUI and run sudo first.
	// User-level items are deferred until sudo completes (or is skipped)
	// so that a cancelled sudo prompt doesn't leave a partial removal.
	if len(sudoItems) > 0 {
		m.state = stateSudo
		m.pendingUserItems = userItems
		script := BuildSudoScript(sudoItems)
		cmd := exec.Command("sudo", "bash", "-c", script)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return sudoDoneMsg{err}
		})
	}

	// No sudo items — remove user-level items directly
	if len(userItems) > 0 {
		m.results = RemoveUserItems(userItems)
	}

	m.state = stateDone
	return m, nil
}

func (m model) selectedCount() int {
	n := 0
	for i := range m.items {
		if m.selected[i] {
			n++
		}
	}
	return n
}

func (m model) selectedSudoCount() int {
	n := 0
	for i, item := range m.items {
		if m.selected[i] && item.NeedsSudo {
			n++
		}
	}
	return n
}

func (m model) selectedTotalSize() int64 {
	var total int64
	for i, item := range m.items {
		if m.selected[i] {
			total += item.Size
		}
	}
	return total
}

// adjustViewport ensures the cursor is within the visible window.
func (m *model) adjustViewport() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	for m.cursor >= m.offset+m.itemsFitFrom(m.offset) {
		m.offset++
	}
}

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

func (m model) viewScanning() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + titleStyle.Render("Cast Screen Cleaner"))
	b.WriteString("  " + dimStyle.Render("macOS"))
	b.WriteString("\n\n")
	b.WriteString("  " + m.spinner.View() + " Scanning system for cast screen app remnants...\n")
	return b.String()
}

func (m model) viewEmpty() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + titleStyle.Render("Cast Screen Cleaner"))
	b.WriteString("  " + dimStyle.Render("macOS"))
	b.WriteString("\n\n")
	b.WriteString("  " + cleanStyle.Render("✓ Your system is clean!"))
	b.WriteString("\n\n")
	b.WriteString("  No cast screen app remnants were found.\n\n")
	b.WriteString("  " + helpStyle.Render("Press q to quit"))
	b.WriteString("\n")
	return b.String()
}

// itemsFitFrom returns how many items fit in the viewport starting from index start,
// accounting for category headers (2 lines each) and per-item lines.
func (m model) itemsFitFrom(start int) int {
	if m.height <= 0 || start >= len(m.items) {
		return max(1, len(m.items)-start)
	}
	available := m.height - 6 // header + footer reserve
	if start > 0 {
		available -= 2 // "↑ N more above" indicator
	}
	if available < 3 {
		return 1
	}

	lines := 0
	count := 0
	prevCat := Category(-1)
	// Force a category header for the first visible item when scrolled
	if start > 0 {
		prevCat = m.items[start].Category - 1
	}

	for i := start; i < len(m.items); i++ {
		item := m.items[i]
		needed := 2 // name line + path line
		if item.Description != "" {
			needed++
		}
		if item.Category != prevCat {
			needed += 2 // blank line + category header
			prevCat = item.Category
		}
		if lines+needed > available {
			break
		}
		lines += needed
		count++
	}

	return max(1, count)
}

func (m model) viewSelecting() string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	b.WriteString("  " + titleStyle.Render("Cast Screen Cleaner"))
	b.WriteString("  " + dimStyle.Render("macOS"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("  Found %s items. Select what to remove:\n",
		accentStyle.Render(fmt.Sprintf("%d", len(m.items)))))

	// Render grouped items
	ruleWidth := m.width - 6
	if ruleWidth < 30 {
		ruleWidth = 30
	}
	if ruleWidth > 72 {
		ruleWidth = 72
	}

	visible := m.itemsFitFrom(m.offset)
	startIdx := m.offset
	endIdx := startIdx + visible
	if endIdx > len(m.items) {
		endIdx = len(m.items)
	}

	// Show scroll-up indicator
	if startIdx > 0 {
		b.WriteString("\n  " + dimStyle.Render(fmt.Sprintf("  ↑ %d more above", startIdx)))
		b.WriteString("\n")
	}

	prevCat := Category(-1)
	if startIdx > 0 {
		// Find the category of the first visible item so we can show its header
		prevCat = m.items[startIdx].Category - 1
	}

	for i := startIdx; i < endIdx; i++ {
		item := m.items[i]

		// Category header
		if item.Category != prevCat {
			prevCat = item.Category
			meta := categoryMeta[item.Category]
			label := fmt.Sprintf(" %s %s ", meta.Icon, meta.Name)
			labelWidth := runewidth.StringWidth(label)
			pad := ruleWidth - labelWidth
			if pad < 4 {
				pad = 4
			}
			b.WriteString("\n")
			b.WriteString("  " + catStyle.Render(label+strings.Repeat("─", pad)))
			b.WriteString("\n")
		}

		// Cursor indicator
		cursor := "  "
		if i == m.cursor {
			cursor = accentStyle.Render("▸ ")
		}

		// Checkbox
		check := uncheckStyle.Render("[ ]")
		if m.selected[i] {
			check = checkStyle.Render("[✓]")
		}

		// Badges
		badges := ""
		if item.NeedsSudo {
			badges += sudoBadge.Render(" sudo")
		}
		if item.IsLoaded {
			badges += loadedStyle.Render(" ● running")
		}

		// Size
		size := dimStyle.Render(item.SizeStr())

		// Line 1: cursor + checkbox + name + badges + size
		b.WriteString(fmt.Sprintf("  %s%s %s%s  %s\n",
			cursor, check, item.Name, badges, size))

		// Line 2: short path
		b.WriteString(fmt.Sprintf("        %s\n", dimStyle.Render(item.ShortPath())))

		// Line 3: description
		if item.Description != "" {
			b.WriteString(fmt.Sprintf("        %s\n", dimStyle.Render(item.Description)))
		}
	}

	// Show scroll-down indicator
	if endIdx < len(m.items) {
		b.WriteString("\n  " + dimStyle.Render(fmt.Sprintf("  ↓ %d more below", len(m.items)-endIdx)))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	sel := m.selectedCount()
	totalSize := formatSize(m.selectedTotalSize())
	b.WriteString("  " + helpStyle.Render(fmt.Sprintf(
		"%d of %d selected (%s) │ ↑↓ move  space toggle  a/n all/none  enter confirm  q quit",
		sel, len(m.items), totalSize)))
	b.WriteString("\n")

	return b.String()
}

func (m model) viewConfirming() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString("  " + warnStyle.Render("⚠  Confirm Removal"))
	b.WriteString("\n\n")
	b.WriteString("  The following items will be removed:\n\n")

	// Collect selected items
	var selected []CleanItem
	for i, item := range m.items {
		if m.selected[i] {
			selected = append(selected, item)
		}
	}

	// Compute viewport for the item list
	overhead := 10 // header + footer lines
	if m.selectedSudoCount() > 0 {
		overhead += 3
	}
	avail := len(selected)
	if m.height > 0 && m.height-overhead < avail {
		avail = max(1, m.height-overhead)
	}
	offset := m.confirmOffset
	if offset > max(0, len(selected)-avail) {
		offset = max(0, len(selected)-avail)
	}
	end := offset + avail
	if end > len(selected) {
		end = len(selected)
	}

	if offset > 0 {
		b.WriteString(fmt.Sprintf("    %s\n", dimStyle.Render(fmt.Sprintf("↑ %d more above", offset))))
	}

	for _, item := range selected[offset:end] {
		sudo := ""
		if item.NeedsSudo {
			sudo = sudoBadge.Render(" (sudo)")
		}
		b.WriteString(fmt.Sprintf("    %s %s%s\n",
			errStyle.Render("✗"),
			item.ShortPath(),
			sudo))
	}

	if end < len(selected) {
		b.WriteString(fmt.Sprintf("    %s\n", dimStyle.Render(fmt.Sprintf("↓ %d more below", len(selected)-end))))
	}

	b.WriteString(fmt.Sprintf("\n  Total: %d items (%s)\n",
		m.selectedCount(), formatSize(m.selectedTotalSize())))

	if m.selectedSudoCount() > 0 {
		b.WriteString("\n  " + warnStyle.Render("⚠  Some items require administrator privileges."))
		b.WriteString("\n     You will be prompted for your password.\n")
	}

	b.WriteString("\n")
	b.WriteString("  " + warnStyle.Render("Press y to confirm"))
	b.WriteString("  " + dimStyle.Render("n/esc to go back, q to quit"))
	b.WriteString("\n")

	return b.String()
}

func (m model) viewDone() string {
	var b strings.Builder

	successes := 0
	for _, r := range m.results {
		if r.Success {
			successes++
		}
	}

	b.WriteString("\n")
	if successes == len(m.results) {
		b.WriteString("  " + okStyle.Render("✓ Removal Complete"))
	} else if successes > 0 {
		b.WriteString("  " + warnStyle.Render("⚠ Removal Partially Complete"))
	} else {
		b.WriteString("  " + errStyle.Render("✗ Removal Failed"))
	}
	b.WriteString("\n\n")

	// Compute viewport for results list
	overhead := 8
	if HasRemovedAudioDriver(m.results) {
		overhead += 4
	}
	avail := len(m.results)
	if m.height > 0 && m.height-overhead < avail {
		avail = max(1, m.height-overhead)
	}
	offset := m.doneOffset
	if offset > max(0, len(m.results)-avail) {
		offset = max(0, len(m.results)-avail)
	}
	end := offset + avail
	if end > len(m.results) {
		end = len(m.results)
	}

	if offset > 0 {
		b.WriteString(fmt.Sprintf("    %s\n", dimStyle.Render(fmt.Sprintf("↑ %d more above", offset))))
	}

	for _, r := range m.results[offset:end] {
		if r.Success {
			b.WriteString(fmt.Sprintf("    %s %s\n",
				okStyle.Render("✓"),
				r.Item.ShortPath()))
		} else {
			b.WriteString(fmt.Sprintf("    %s %s  %s\n",
				errStyle.Render("✗"),
				r.Item.ShortPath(),
				dimStyle.Render(r.Error)))
		}
	}

	if end < len(m.results) {
		b.WriteString(fmt.Sprintf("    %s\n", dimStyle.Render(fmt.Sprintf("↓ %d more below", len(m.results)-end))))
	}

	b.WriteString(fmt.Sprintf("\n  %d of %d items successfully removed.\n",
		successes, len(m.results)))

	if HasRemovedAudioDriver(m.results) {
		b.WriteString("\n")
		b.WriteString("  " + dimStyle.Render("ℹ  Audio driver removed. Core Audio will reset automatically,"))
		b.WriteString("\n")
		b.WriteString("  " + dimStyle.Render("   or run: sudo killall coreaudiod"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString("  " + helpStyle.Render("Press q or enter to exit"))
	b.WriteString("\n")

	return b.String()
}
