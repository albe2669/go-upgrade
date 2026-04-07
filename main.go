package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// goModule represents the JSON output of `go list -json -u -m all`.
type goModule struct {
	Path       string    `json:"Path"`
	Version    string    `json:"Version"`
	Update     *goModule `json:"Update"`
	Indirect   bool      `json:"Indirect"`
	Main       bool      `json:"Main"`
	Deprecated string    `json:"Deprecated"`
}

// Dependency is a direct module with an available update.
type Dependency struct {
	Path       string
	Current    string
	NewVersion string
	Deprecated string
}

type phase int

const (
	phaseLoading  phase = iota
	phaseSelect
	phaseUpdating
	phaseDone
)

type depsLoadedMsg struct {
	deps []Dependency
	err  error
}

type depUpdatedMsg struct {
	index int
	err   error
}

type tidyDoneMsg struct {
	err error
}

type model struct {
	phase    phase
	deps     []Dependency
	selected map[int]bool
	cursor   int
	spinner  spinner.Model
	progress progress.Model
	err      error

	// Updating state
	updateOrder []int    // indices of selected deps, in order
	updatePos   int      // current position in updateOrder
	updateErrs  []string // per-dep errors (empty string = success)
	tidyErr     error
	quitting    bool

	// For display alignment
	maxPathLen int
}

// parseDeps parses concatenated JSON objects from `go list -json -u -m all`
// and returns only direct dependencies with available updates.
func parseDeps(r io.Reader) ([]Dependency, error) {
	dec := json.NewDecoder(r)
	var deps []Dependency
	for {
		var m goModule
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parsing module JSON: %w", err)
		}
		if m.Main || m.Indirect || m.Update == nil {
			continue
		}
		deps = append(deps, Dependency{
			Path:       m.Path,
			Current:    m.Version,
			NewVersion: m.Update.Version,
			Deprecated: m.Deprecated,
		})
	}
	return deps, nil
}

func loadDepsCmd() tea.Msg {
	cmd := exec.Command("go", "list", "-json", "-u", "-m", "all")
	out, err := cmd.Output()
	if err != nil {
		return depsLoadedMsg{err: fmt.Errorf("go list: %w", err)}
	}
	deps, err := parseDeps(strings.NewReader(string(out)))
	return depsLoadedMsg{deps: deps, err: err}
}

func updateDepCmd(index int, dep Dependency) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("go", "get", dep.Path+"@"+dep.NewVersion)
		err := cmd.Run()
		return depUpdatedMsg{index: index, err: err}
	}
}

func tidyCmd() tea.Msg {
	cmd := exec.Command("go", "mod", "tidy")
	err := cmd.Run()
	return tidyDoneMsg{err: err}
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
	cursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
	checkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	uncheckStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	pathStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
	dimPathStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	oldVerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	newVerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	arrowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	deprStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	hintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	spinnerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))
	progressStyle = lipgloss.NewStyle().Padding(1, 0)
)

func initialModel() model {
	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(spinnerStyle),
	)
	p := progress.New(
		progress.WithDefaultBlend(),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)
	return model{
		phase:    phaseLoading,
		selected: make(map[int]bool),
		spinner:  s,
		progress: p,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, loadDepsCmd)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.phase {
	case phaseLoading:
		return m.updateLoading(msg)
	case phaseSelect:
		return m.updateSelect(msg)
	case phaseUpdating:
		return m.updateUpdating(msg)
	case phaseDone:
		return m.updateDone(msg)
	}
	return m, nil
}

func (m model) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			m.quitting = true
			return m, tea.Quit
		}
	case depsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.phase = phaseDone
			return m, nil
		}
		if len(msg.deps) == 0 {
			m.phase = phaseDone
			return m, nil
		}
		m.deps = msg.deps
		m.maxPathLen = 0
		for _, d := range m.deps {
			if len(d.Path) > m.maxPathLen {
				m.maxPathLen = len(d.Path)
			}
		}
		m.phase = phaseSelect
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.deps)-1 {
				m.cursor++
			}
		case " ", "space":
			m.selected[m.cursor] = !m.selected[m.cursor]
		case "a":
			allSelected := len(m.selected) == len(m.deps)
			if !allSelected {
				// Check if truly all selected
				allSelected = true
				for i := range m.deps {
					if !m.selected[i] {
						allSelected = false
						break
					}
				}
			}
			if allSelected {
				m.selected = make(map[int]bool)
			} else {
				for i := range m.deps {
					m.selected[i] = true
				}
			}
		case "enter":
			// Collect selected indices
			var order []int
			for i := range m.deps {
				if m.selected[i] {
					order = append(order, i)
				}
			}
			if len(order) == 0 {
				return m, nil
			}
			m.updateOrder = order
			m.updatePos = 0
			m.updateErrs = make([]string, len(m.deps))
			m.phase = phaseUpdating
			idx := m.updateOrder[0]
			return m, tea.Batch(
				m.spinner.Tick,
				updateDepCmd(idx, m.deps[idx]),
			)
		}
	}
	return m, nil
}

func (m model) updateUpdating(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	case depUpdatedMsg:
		if msg.err != nil {
			m.updateErrs[msg.index] = msg.err.Error()
		}
		m.updatePos++

		// Update progress
		pct := float64(m.updatePos) / float64(len(m.updateOrder))
		cmd := m.progress.SetPercent(pct)

		if m.updatePos >= len(m.updateOrder) {
			// All done, run tidy
			return m, tea.Batch(cmd, tidyCmd)
		}
		idx := m.updateOrder[m.updatePos]
		return m, tea.Batch(cmd, updateDepCmd(idx, m.deps[idx]))
	case tidyDoneMsg:
		m.tidyErr = msg.err
		m.phase = phaseDone
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case progress.FrameMsg:
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		_ = msg
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	var s strings.Builder

	switch m.phase {
	case phaseLoading:
		s.WriteString(fmt.Sprintf("\n  %s Checking for dependency updates...\n\n", m.spinner.View()))

	case phaseSelect:
		s.WriteString(titleStyle.Render("  Select dependencies to update"))
		s.WriteString("\n")
		s.WriteString(hintStyle.Render("  space: toggle  a: all  enter: confirm  q: quit"))
		s.WriteString("\n\n")

		selectedCount := 0
		for _, v := range m.selected {
			if v {
				selectedCount++
			}
		}

		for i, dep := range m.deps {
			cursor := "  "
			if m.cursor == i {
				cursor = cursorStyle.Render("> ")
			}

			check := uncheckStyle.Render("[ ]")
			pStyle := dimPathStyle
			if m.selected[i] {
				check = checkStyle.Render("[x]")
				pStyle = pathStyle
			}

			paddedPath := fmt.Sprintf("%-*s", m.maxPathLen, dep.Path)
			line := fmt.Sprintf("%s%s %s  %s %s %s",
				cursor,
				check,
				pStyle.Render(paddedPath),
				oldVerStyle.Render(dep.Current),
				arrowStyle.Render("->"),
				newVerStyle.Render(dep.NewVersion),
			)

			if dep.Deprecated != "" {
				line += " " + deprStyle.Render("[DEPRECATED]")
			}

			s.WriteString(line)
			s.WriteString("\n")
		}

		s.WriteString(fmt.Sprintf("\n  %s %d/%d selected\n",
			hintStyle.Render(""),
			selectedCount,
			len(m.deps),
		))

	case phaseUpdating:
		total := len(m.updateOrder)
		done := m.updatePos
		s.WriteString(fmt.Sprintf("\n  %s Updating dependencies... %d/%d\n",
			m.spinner.View(), done, total))
		s.WriteString(fmt.Sprintf("  %s\n\n", m.progress.View()))

		for i, idx := range m.updateOrder {
			dep := m.deps[idx]
			paddedPath := fmt.Sprintf("%-*s", m.maxPathLen, dep.Path)
			if i < m.updatePos {
				// Done
				status := successStyle.Render("  OK")
				if m.updateErrs[idx] != "" {
					status = errorStyle.Render("FAIL")
				}
				s.WriteString(fmt.Sprintf("  %s %s  %s %s %s\n",
					status,
					pathStyle.Render(paddedPath),
					oldVerStyle.Render(dep.Current),
					arrowStyle.Render("->"),
					newVerStyle.Render(dep.NewVersion),
				))
			} else if i == m.updatePos {
				// In progress
				s.WriteString(fmt.Sprintf("  %s %s  %s %s %s\n",
					m.spinner.View(),
					pathStyle.Render(paddedPath),
					oldVerStyle.Render(dep.Current),
					arrowStyle.Render("->"),
					newVerStyle.Render(dep.NewVersion),
				))
			} else {
				// Pending
				s.WriteString(fmt.Sprintf("       %s  %s %s %s\n",
					dimPathStyle.Render(paddedPath),
					oldVerStyle.Render(dep.Current),
					arrowStyle.Render("->"),
					newVerStyle.Render(dep.NewVersion),
				))
			}
		}
		s.WriteString("\n")

	case phaseDone:
		if m.err != nil {
			s.WriteString(fmt.Sprintf("\n  %s %s\n",
				errorStyle.Render("Error:"),
				m.err.Error(),
			))
			s.WriteString(hintStyle.Render("\n  Press any key to exit.\n\n"))
			return tea.NewView(s.String())
		}

		if len(m.deps) == 0 {
			s.WriteString(successStyle.Render("\n  All dependencies are up to date!\n"))
			s.WriteString(hintStyle.Render("\n  Press any key to exit.\n\n"))
			return tea.NewView(s.String())
		}

		// Show results
		successCount := 0
		failCount := 0
		for _, idx := range m.updateOrder {
			if m.updateErrs[idx] == "" {
				successCount++
			} else {
				failCount++
			}
		}

		if failCount == 0 {
			s.WriteString(successStyle.Render(fmt.Sprintf("\n  Done! Updated %d dependencies.\n\n", successCount)))
		} else {
			s.WriteString(fmt.Sprintf("\n  Done. %s, %s.\n\n",
				successStyle.Render(fmt.Sprintf("%d updated", successCount)),
				errorStyle.Render(fmt.Sprintf("%d failed", failCount)),
			))
		}

		for _, idx := range m.updateOrder {
			dep := m.deps[idx]
			paddedPath := fmt.Sprintf("%-*s", m.maxPathLen, dep.Path)
			if m.updateErrs[idx] == "" {
				s.WriteString(fmt.Sprintf("  %s %s  %s %s %s\n",
					successStyle.Render("  OK"),
					pathStyle.Render(paddedPath),
					oldVerStyle.Render(dep.Current),
					arrowStyle.Render("->"),
					newVerStyle.Render(dep.NewVersion),
				))
			} else {
				s.WriteString(fmt.Sprintf("  %s %s  %s %s %s\n",
					errorStyle.Render("FAIL"),
					pathStyle.Render(paddedPath),
					oldVerStyle.Render(dep.Current),
					arrowStyle.Render("->"),
					newVerStyle.Render(dep.NewVersion),
				))
				s.WriteString(fmt.Sprintf("       %s\n", errorStyle.Render(m.updateErrs[idx])))
			}
		}

		if m.tidyErr != nil {
			s.WriteString(fmt.Sprintf("\n  %s go mod tidy: %s\n",
				errorStyle.Render("Warning:"),
				m.tidyErr.Error(),
			))
		}

		s.WriteString(hintStyle.Render("\n  Press any key to exit.\n\n"))
	}

	return tea.NewView(s.String())
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
