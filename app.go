package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
)

var (
	// Step state styles (no padding here — leftPaneStyle handles it)
	stepPendingStyle  = lipgloss.NewStyle()
	stepRunningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	stepSuccessStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	stepFailedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	stepSkippedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Strikethrough(true)

	// Pane styles
	leftPaneStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1)
	paneStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()) // no padding, just border

	// Title and label styles
	paneTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
	paramLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("241"))
	paramUsedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	paramUnusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Background(lipgloss.Color("235")).Padding(0, 1)
	sessionStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("235")).Padding(0, 1)
)

type model struct {
	workflow     *Workflow
	session      *Session
	workflowDir  string

	cursor       int
	paramInputs  map[string]textinput.Model
	paramNames   []string
	focusedParam int

	stdoutViewport viewport.Model
	stderrViewport viewport.Model

	width  int
	height int

	skipConfirm     bool
	showSessionList bool
	sessionList     []*Session
	sessionCursor   int

	runner        *stepRunner
	currentStepID string
	stdoutBuffer  []byte
	stderrBuffer  []byte
}

func initialModel(wf *Workflow, session *Session, workflowDir string) model {
	m := model{
		workflow:     wf,
		session:      session,
		workflowDir:  workflowDir,
		paramInputs:  make(map[string]textinput.Model),
		paramNames:   make([]string, 0, len(wf.Parameters)),
		focusedParam: -1,
	}
	for name := range wf.Parameters {
		m.paramNames = append(m.paramNames, name)
	}
	sort.Strings(m.paramNames)
	m.updateParamInputs()
	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

type errMsg struct{ err error }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewports()
		m.updateParamInputWidths()

	case shellStdoutMsg:
		m.stdoutBuffer = append(m.stdoutBuffer, msg.line...)
		m.stdoutViewport.SetContent(string(m.stdoutBuffer))
		m.stdoutViewport.GotoBottom()
		if m.runner != nil {
			return m, m.runner.NextCmd()
		}

	case shellStderrMsg:
		m.stderrBuffer = append(m.stderrBuffer, msg.line...)
		m.stderrViewport.SetContent(string(m.stderrBuffer))
		m.stderrViewport.GotoBottom()
		if m.runner != nil {
			return m, m.runner.NextCmd()
		}

	case shellDoneMsg:
		// Drain any remaining stdout/stderr before saving
		if m.runner != nil {
			stdoutLines, stderrLines := m.runner.Drain()
			for _, line := range stdoutLines {
				m.stdoutBuffer = append(m.stdoutBuffer, line...)
			}
			for _, line := range stderrLines {
				m.stderrBuffer = append(m.stderrBuffer, line...)
			}
		}
		m.session.UpdateStepState(msg.stepID, StepState{
			Status:   msg.status,
			ExitCode: msg.exitCode,
			Stdout:   string(m.stdoutBuffer),
			Stderr:   string(m.stderrBuffer),
		})
		m.runner = nil
		m.currentStepID = ""
		m.refreshStdoutContent()
		// For markdown output, scroll to top so the user sees the beginning
		if m.workflow != nil && m.cursor < len(m.workflow.Steps) && m.workflow.Steps[m.cursor].OutputType == OutputMarkdown {
			m.stdoutViewport.SetYOffset(0)
		}
		return m, m.autoSave()

	case errMsg:
		m.stderrBuffer = append(m.stderrBuffer, fmt.Sprintf("\nError: %v\n", msg.err)...)
		m.stderrViewport.SetContent(string(m.stderrBuffer))
		m.stderrViewport.GotoBottom()

	case tea.KeyMsg:
		if m.skipConfirm {
			switch msg.String() {
			case "y", "Y":
				m.skipCurrentStep()
				m.skipConfirm = false
				return m, m.autoSave()
			case "n", "N", "q", "esc":
				m.skipConfirm = false
				return m, nil
			}
			return m, nil
		}

		if m.showSessionList {
			switch msg.String() {
			case "q", "esc":
				m.showSessionList = false
				return m, nil
			case "n":
				m.session = NewSession(m.workflow, m.session.Cwd)
				m.cursor = 0
				m.updateParamInputs()
				m.stdoutBuffer = nil
				m.stderrBuffer = nil
				m.stdoutViewport.SetContent("")
				m.stderrViewport.SetContent("")
				m.showSessionList = false
				return m, m.autoSave()
			case "up", "k":
				if m.sessionCursor > 0 {
					m.sessionCursor--
				}
			case "down", "j":
				if m.sessionCursor < len(m.sessionList)-1 {
					m.sessionCursor++
				}
			case "enter":
				if m.sessionCursor < len(m.sessionList) {
					m.session = m.sessionList[m.sessionCursor]
					m.cursor = 0
					m.updateParamInputs()
					m.loadStepOutput()
					m.showSessionList = false
					return m, nil
				}
			}
			return m, nil
		}

		if m.focusedParam >= 0 {
			if msg.String() == "tab" {
				m.focusedParam = (m.focusedParam + 1) % len(m.paramNames)
				return m, m.blurAllExcept(m.focusedParam)
			}
			if msg.String() == "shift+tab" {
				m.focusedParam--
				if m.focusedParam < 0 {
					m.focusedParam = len(m.paramNames) - 1
				}
				return m, m.blurAllExcept(m.focusedParam)
			}
			if msg.String() == "esc" {
				m.focusedParam = -1
				return m, m.blurAllParams()
			}
			name := m.paramNames[m.focusedParam]
			input, ok := m.paramInputs[name]
			if ok {
				newInput, cmd := input.Update(msg)
				m.paramInputs[name] = newInput
				m.session.SetParameterValue(name, newInput.Value())
				cmds = append(cmds, cmd, m.autoSave())
			}
			return m, tea.Batch(cmds...)
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.loadStepOutput()
			}
		case "down", "j":
			if m.workflow != nil && m.cursor < len(m.workflow.Steps)-1 {
				m.cursor++
				m.loadStepOutput()
			}
		case "tab":
			if len(m.paramNames) > 0 {
				m.focusedParam = 0
				return m, m.blurAllExcept(0)
			}
		case "r":
			if m.canRun() {
				return m, m.runCurrentStep()
			}
		case "d":
			if m.canSkip() {
				m.skipConfirm = true
			}
		case "s":
			m.showSessionList = true
			m.sessionCursor = 0
			m.sessionList, _ = FindSessionsForWorkflow(m.workflow.Name, m.session.Cwd)
		case "pgup":
			m.stdoutViewport.PageUp()
		case "pgdown":
			m.stdoutViewport.PageDown()
		case "home":
			m.stdoutViewport.GotoTop()
		case "end":
			m.stdoutViewport.GotoBottom()
		default:
			// Pass unhandled keys to the viewport for scrolling
			var cmd tea.Cmd
			m.stdoutViewport, cmd = m.stdoutViewport.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			m.stderrViewport, cmd = m.stderrViewport.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		v := tea.NewView("Loading...")
		v.AltScreen = true
		return v
	}

	if m.showSessionList {
		v := tea.NewView(m.renderSessionList())
		v.AltScreen = true
		return v
	}

	if m.skipConfirm {
		v := tea.NewView(m.renderSkipConfirm())
		v.AltScreen = true
		return v
	}

	paneFrameH := paneStyle.GetHorizontalFrameSize()

	leftW := m.leftWidth()
	rightW := m.rightWidth()

	leftContentW := max(2, leftW-leftPaneStyle.GetHorizontalFrameSize())
	stepsContentH := max(3, m.height-10)
	infoContentH := 2

	leftContentRaw := m.renderStepListContent(leftContentW)
	stepsPane := leftPaneStyle.Width(leftContentW).Height(stepsContentH).Render(leftContentRaw)
	infoPane := leftPaneStyle.Width(leftContentW).Height(infoContentH).Render(m.renderStepInfo(leftContentW))
	left := lipgloss.JoinVertical(lipgloss.Left, stepsPane, infoPane)

	rightContentW := max(2, rightW-paneFrameH)
	paramsContent := m.renderParamContent(rightContentW)
	stderrContent := m.stderrViewport.View()

	// For markdown output, bypass the viewport's broken MaxWidth truncation
	// and the pane's Width wrapping (both are not ANSI-aware in lipgloss).
	// Glamour already wraps at the correct width, so we render directly.
	var stdoutContent string
	var stdout string
	if m.workflow != nil && m.cursor < len(m.workflow.Steps) && m.workflow.Steps[m.cursor].OutputType == OutputMarkdown {
		stdoutContent = m.renderViewportContent(m.stdoutViewport)
		stdout = paneStyle.Render(
			paneTitleStyle.Render("Stdout") + "\n" + stdoutContent)
	} else {
		stdoutContent = m.stdoutViewport.View()
		stdout = paneStyle.Width(max(2, rightW-paneFrameH)).Render(
			paneTitleStyle.Render("Stdout") + "\n" + stdoutContent)
	}

	params := paneStyle.Width(max(2, rightW-paneFrameH)).Render(
		paneTitleStyle.Render("Parameters") + "\n" + paramsContent)
	stderr := paneStyle.Width(max(2, rightW-paneFrameH)).Render(
		paneTitleStyle.Render("Stderr") + "\n" + stderrContent)

	right := lipgloss.JoinVertical(lipgloss.Left, params, stdout, stderr)

	// Render title bar
	titleBar := m.renderTitle()

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	footer := lipgloss.NewStyle().Height(1).Render(
		"↑/↓ nav  r run  d skip  tab params  s sessions  pgup/pgdn scroll  q quit",
	)

	all := lipgloss.JoinVertical(lipgloss.Left, titleBar, body, footer)
	v := tea.NewView(all)
	v.AltScreen = true
	return v
}

// Cursor returns the cursor position for the focused text input.
func (m model) Cursor() *tea.Cursor {
	if m.focusedParam >= 0 && m.focusedParam < len(m.paramNames) {
		name := m.paramNames[m.focusedParam]
		input := m.paramInputs[name]
		if input.Focused() {
			c := input.Cursor()
			// Find the X position of the input in the right pane
			leftW := m.leftWidth()
			// X offset: left pane width + border
			c.X += leftW + 1
			// Y offset: parameters pane height + title + border + lines before input
			paramLines := m.paramLines()
			c.Y += paramLines + 3 + m.focusedParam*3
			return c
		}
	}
	return nil
}

// --- Layout ---

func (m model) leftWidth() int {
	w := m.width / 3
	if w > 30 {
		w = 30
	}
	return max(w, 10)
}

func (m model) rightWidth() int {
	return max(m.width-m.leftWidth(), 10)
}

func (m model) leftContentW() int {
	leftFrameH := leftPaneStyle.GetHorizontalFrameSize()
	return max(2, m.leftWidth()-leftFrameH)
}

func (m model) paramLines() int {
	if len(m.paramNames) == 0 {
		return 1
	}
	return len(m.paramNames) * 3
}

func (m *model) resizeViewports() {
	paneFrameH := paneStyle.GetHorizontalFrameSize()
	paneFrameV := paneStyle.GetVerticalFrameSize()
	viewportW := max(2, m.rightWidth()-paneFrameH)

	// Calculate viewport heights based on available space
	paramLines := m.paramLines()
	if len(m.paramNames) == 0 {
		paramLines = 1
	}
	paramPaneContent := paramLines + 1 // +1 for title line
	// Overhead: 3 pane borders + 2 title lines for stdout/stderr + title bar
	// (the params title is already counted in paramPaneContent)
	totalOverhead := 3*paneFrameV + 2
	remaining := m.height - 2 - paramPaneContent - totalOverhead

	var stdoutVH, stderrVH int
	if remaining < 6 {
		stdoutVH = 3
		stderrVH = 3
	} else {
		stdoutVH = max(3, remaining/2)
		stderrVH = max(3, remaining-stdoutVH)
	}

	m.stdoutViewport = viewport.New(viewport.WithWidth(viewportW), viewport.WithHeight(stdoutVH))
	m.stderrViewport = viewport.New(viewport.WithWidth(viewportW), viewport.WithHeight(stderrVH))

	// If the current step is markdown, re-render with new width
	m.refreshStdoutContent()
}

// refreshStdoutContent sets the viewport content, rendering markdown if needed.
func (m *model) refreshStdoutContent() {
	paneFrameH := paneStyle.GetHorizontalFrameSize()
	normalWidth := max(2, m.rightWidth()-paneFrameH)
	stdoutStr := string(m.stdoutBuffer)

	if m.workflow != nil && m.cursor < len(m.workflow.Steps) && m.currentStepID != m.workflow.Steps[m.cursor].ID {
		step := m.workflow.Steps[m.cursor]
		if step.OutputType == OutputMarkdown && stdoutStr != "" {
			rendered, err := m.renderMarkdown(stdoutStr, normalWidth)
			if err == nil {
				stdoutStr = rendered
			}
			m.stdoutViewport.SetContent(stdoutStr)
			m.stderrViewport.SetContent(string(m.stderrBuffer))
			return
		}
	}

	// Normal width for non-markdown content
	m.stdoutViewport.SetWidth(normalWidth)
	m.stdoutViewport.SetContent(stdoutStr)
	m.stderrViewport.SetContent(string(m.stderrBuffer))
}

// --- Content renderers ---

func (m model) renderStepListContent(w int) string {
	if m.workflow == nil {
		return "No workflow"
	}
	if m.session == nil {
		return "No session"
	}

	var lines []string
	lines = append(lines, paneTitleStyle.Render("Steps"))
	lines = append(lines, "")

	for i, step := range m.workflow.Steps {
		state, ok := m.session.StepStates[step.ID]
		if !ok {
			state = StepState{Status: StatusPending}
		}

		style := stepPendingStyle
		statusText := "pending"
		switch state.Status {
		case StatusRunning:
			style = stepRunningStyle
			statusText = "running"
		case StatusSuccess:
			style = stepSuccessStyle
			statusText = "done"
		case StatusFailed:
			style = stepFailedStyle
			statusText = "failed"
		case StatusSkipped:
			style = stepSkippedStyle
			statusText = "skipped"
		}

		prefix := "  "
		if i == m.cursor {
			prefix = "> "
			style = style.Copy().Background(lipgloss.Color("236")).Bold(true)
		}

		icon := m.statusIcon(state.Status)
		runIcon := m.runTypeIcon(step)
		line := style.Copy().MaxWidth(w).Render(fmt.Sprintf("%s%s %s %s — %s", prefix, icon, runIcon, step.Name, statusText))
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return content
}

// renderStepInfo returns a short info block for the currently selected step.
func (m model) renderStepInfo(w int) string {
	if m.workflow == nil || m.session == nil || m.cursor >= len(m.workflow.Steps) {
		return ""
	}
	step := m.workflow.Steps[m.cursor]
	state := m.session.StepStates[step.ID]

	desc := step.Description
	if desc == "" {
		desc = "(no description)"
	}
	lastRun := "Never"
	if state.RunAt != "" {
		lastRun = state.RunAt
	}

	descLine := lipgloss.NewStyle().MaxWidth(w).Render(desc)
	runLine := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).MaxWidth(w).Render("Last run: " + lastRun)
	return descLine + "\n" + runLine
}

func (m model) statusIcon(status StepStatus) string {
	switch status {
	case StatusPending:
		return "○"
	case StatusRunning:
		return "●"
	case StatusSuccess:
		return "✓"
	case StatusFailed:
		return "✗"
	case StatusSkipped:
		return "⊘"
	}
	return "?"
}

func (m model) runTypeIcon(step Step) string {
	if step.RunOncePerSession {
		return "⊘"
	}
	return "↻"
}

func (m model) renderParamContent(w int) string {
	if len(m.paramNames) == 0 {
		return "No parameters"
	}

	var lines []string
	for i, name := range m.paramNames {
		param, ok := m.workflow.Parameters[name]
		if !ok {
			continue
		}
		input, ok := m.paramInputs[name]
		if !ok {
			continue
		}

		used := false
		if m.cursor < len(m.workflow.Steps) {
			for _, p := range m.workflow.Steps[m.cursor].Params {
				if p == name {
					used = true
					break
				}
			}
		}

		labelStyle := paramUnusedStyle
		if used {
			labelStyle = paramUsedStyle
		}
		if i == m.focusedParam {
			labelStyle = labelStyle.Copy().Underline(true)
		}

		label := labelStyle.MaxWidth(w).Render(fmt.Sprintf("%s: %s", name, param.Description))
		lines = append(lines, label, input.View(), "")
	}

	return strings.Join(lines, "\n")
}

func (m model) renderSessionList() string {
	var lines []string
	lines = append(lines, paneTitleStyle.Render("Sessions for this workflow"), "")
	for i, sess := range m.sessionList {
		cursor := "  "
		if i == m.sessionCursor {
			cursor = "> "
		}
		status := sess.OverallStatus()
		statusStyle := lipgloss.NewStyle()
		switch status {
		case "done":
			statusStyle = statusStyle.Foreground(lipgloss.Color("42"))
		case "failed":
			statusStyle = statusStyle.Foreground(lipgloss.Color("196"))
		case "running":
			statusStyle = statusStyle.Foreground(lipgloss.Color("33"))
		case "pending":
			statusStyle = statusStyle.Foreground(lipgloss.Color("244"))
		default:
			statusStyle = statusStyle.Foreground(lipgloss.Color("250"))
		}
		// Format the datetime for display: 2006-01-02T15-04-05.000 -> 2006-01-02 15:04:05
		displayName := strings.Replace(sess.Name, "T", " ", 1)
		if idx := strings.LastIndex(displayName, "."); idx > 0 {
			displayName = displayName[:idx]
		}
		line := fmt.Sprintf("%s%s (%s)", cursor, displayName, statusStyle.Render(status))
		lines = append(lines, line)
	}
	if len(m.sessionList) == 0 {
		lines = append(lines, "  (none)")
	}
	lines = append(lines, "", "enter pick  n new  q/esc close")

	modalW := min(60, m.width-4)
	modalH := min(m.height-4, len(lines)+2)
	contentW := max(2, modalW-leftPaneStyle.GetHorizontalFrameSize())
	contentH := max(1, modalH-leftPaneStyle.GetVerticalFrameSize())
	content := lipgloss.NewStyle().MaxWidth(contentW).MaxHeight(contentH).Render(strings.Join(lines, "\n"))
	overlay := leftPaneStyle.Width(contentW).Height(contentH).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
}

func (m model) renderSkipConfirm() string {
	step := m.workflow.Steps[m.cursor]
	msg := fmt.Sprintf("Skip step %q?\n\n(y/n)", step.Name)
	modalStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1).Width(50).Align(lipgloss.Center)
	overlay := modalStyle.Render(msg)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
}

// --- Logic ---

func (m *model) skipCurrentStep() {
	if m.workflow == nil || m.session == nil {
		return
	}
	step := m.workflow.Steps[m.cursor]
	m.session.UpdateStepState(step.ID, StepState{Status: StatusSkipped})
}

// loadStepOutput populates the stdout/stderr buffers from the currently selected step.
func (m *model) loadStepOutput() {
	if m.workflow == nil || m.session == nil || m.cursor >= len(m.workflow.Steps) {
		m.stdoutBuffer = nil
		m.stderrBuffer = nil
		m.stdoutViewport.SetContent("")
		m.stderrViewport.SetContent("")
		return
	}
	step := m.workflow.Steps[m.cursor]
	state := m.session.StepStates[step.ID]
	m.stdoutBuffer = []byte(state.Stdout)
	m.stderrBuffer = []byte(state.Stderr)
	// Backward compat: if new fields are empty, try old Output field
	if m.stdoutBuffer == nil && m.stderrBuffer == nil && state.Output != "" {
		out, err := state.GetOutput()
		m.stdoutBuffer = []byte(out)
		m.stderrBuffer = []byte(err)
	}
	m.refreshStdoutContent()
	// For markdown, scroll to top so the beginning of the document is visible
	if step.OutputType == OutputMarkdown {
		m.stdoutViewport.SetYOffset(0)
	}
}

// renderMarkdown renders markdown content via glamour.
func (m model) renderMarkdown(content string, width int) (string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithStandardStyle("dark"),
	)
	if err != nil {
		return "", err
	}
	return renderer.Render(content)
}

// renderViewportContent returns the visible lines of a viewport without
// applying the viewport's MaxWidth truncation (which is not ANSI-aware).
func (m model) renderViewportContent(vp viewport.Model) string {
	lines := strings.Split(vp.GetContent(), "\n")
	yOffset := vp.YOffset()
	height := vp.Height()
	top := max(0, yOffset)
	bottom := min(len(lines), top+height)
	if top >= len(lines) {
		return ""
	}
	return strings.Join(lines[top:bottom], "\n")
}

// renderTitle returns the title bar showing workflow name, description, and session name.
func (m model) renderTitle() string {
	if m.workflow == nil || m.session == nil {
		return ""
	}
	wfName := m.workflow.Name
	wfDesc := m.workflow.Description
	sessionName := m.session.Name

	var parts []string
	parts = append(parts, titleStyle.Render(wfName))
	if wfDesc != "" {
		parts = append(parts, sessionStyle.Render("—"))
		parts = append(parts, sessionStyle.Render(wfDesc))
	}
	parts = append(parts, sessionStyle.Render("["+sessionName+"]"))

	title := lipgloss.JoinHorizontal(lipgloss.Center, parts...)
	return lipgloss.NewStyle().Width(m.width).Render(title)
}

func (m model) canRun() bool {
	if m.workflow == nil || m.session == nil {
		return false
	}
	return m.session.IsStepRunnable(m.workflow, m.cursor)
}

func (m model) canSkip() bool {
	if m.workflow == nil || m.session == nil {
		return false
	}
	step := m.workflow.Steps[m.cursor]
	state := m.session.StepStates[step.ID]
	return step.RunOncePerSession && state.Status == StatusPending
}

func (m *model) updateParamInputs() {
	if m.workflow == nil {
		return
	}
	paneFrameH := paneStyle.GetHorizontalFrameSize()
	for name, param := range m.workflow.Parameters {
		val := m.session.GetParameterValue(name, m.workflow)
		input := textinput.New()
		input.Prompt = ""
		input.Placeholder = param.Default
		input.SetValue(val)
		input.SetWidth(max(2, m.rightWidth()-paneFrameH))
		m.paramInputs[name] = input
	}
	m.updateParamInputWidths()
}

func (m *model) updateParamInputWidths() {
	paneFrameH := paneStyle.GetHorizontalFrameSize()
	w := max(2, m.rightWidth()-paneFrameH)
	for name, input := range m.paramInputs {
		input.SetWidth(w)
		m.paramInputs[name] = input
	}
}

func (m *model) blurAllParams() tea.Cmd {
	for name, input := range m.paramInputs {
		input.Blur()
		m.paramInputs[name] = input
	}
	return nil
}

func (m *model) blurAllExcept(idx int) tea.Cmd {
	for i, name := range m.paramNames {
		input := m.paramInputs[name]
		if i == idx {
			input.Focus()
		} else {
			input.Blur()
		}
		m.paramInputs[name] = input
	}
	return func() tea.Msg { return textinput.Blink() }
}

func (m *model) autoSave() tea.Cmd {
	if m.session == nil {
		return nil
	}
	return func() tea.Msg {
		if err := SaveSession(m.session); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m *model) runCurrentStep() tea.Cmd {
	if m.workflow == nil || m.session == nil {
		return nil
	}
	step := m.workflow.Steps[m.cursor]
	scriptPath := ResolveScriptPath(m.workflowDir, step.Script)
	if _, err := os.Stat(scriptPath); err != nil {
		m.stderrBuffer = append(m.stderrBuffer, fmt.Sprintf("Script not found: %s\n", scriptPath)...)
		m.stderrViewport.SetContent(string(m.stderrBuffer))
		m.stderrViewport.GotoBottom()
		return nil
	}
	m.session.UpdateStepState(step.ID, StepState{Status: StatusRunning})
	m.stdoutBuffer = nil
	m.stderrBuffer = nil
	m.stdoutViewport.SetContent("")
	m.stderrViewport.SetContent("")
	m.currentStepID = step.ID

	params := buildParams(step, m)
	m.runner = newStepRunner(step, m.workflowDir, params)
	return tea.Batch(m.autoSave(), m.runner.NextCmd())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
