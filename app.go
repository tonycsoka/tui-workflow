package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
)

var (
	// Step state styles (no padding here — leftPaneStyle handles it)
	stepPendingStyle = lipgloss.NewStyle()
	stepRunningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	stepSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	stepFailedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	stepSkippedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Strikethrough(true)

	// Pane styles
	leftPaneStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	paneStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())

	// Title and label styles
	paneTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
	paramLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("241"))
	paramUsedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	paramUnusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Background(lipgloss.Color("235")).Padding(0, 1)
	sessionStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("235")).Padding(0, 1)
)

// Layout constants for the TUI.
const (
	leftPaneMaxWidth   = 45
	leftPaneMinWidth   = 15
	rightPaneMinWidth  = 10
	stepsPaneMinHeight = 3
	infoPaneHeight     = 2
	paramBlockHeight   = 3 // label + input + spacing
	modalMaxWidth      = 60

	titleBarHeight = 1
	footerHeight   = 1
	cursorBgColor  = "236"
	lastRunFgColor = "244"
)

// liveOutput holds the raw stdout/stderr for a step that is currently running.
// This decouples the live stream from the currently selected step so the user
// can navigate away without losing the running step's output.
type liveOutput struct {
	stdout []byte
	stderr []byte
}

type model struct {
	workflow    *Workflow
	session     *Session
	workflowDir string

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

	liveOutputs map[string]*liveOutput // per-step buffers for running steps

	mdRendererCache map[int]*glamour.TermRenderer // cached glamour renderers per width
	mdViewportLines []string                      // cached split of markdown content

	autoRun     bool   // chain auto-run mode active
	savePending bool   // debounce flag for autoSave
	saveErr     string // transient error from autoSave
}

func initialModel(wf *Workflow, session *Session, workflowDir string) model {
	m := model{
		workflow:     wf,
		session:      session,
		workflowDir:  workflowDir,
		paramInputs:  make(map[string]textinput.Model),
		paramNames:   make([]string, 0, len(wf.Parameters)),
		focusedParam: -1,
		liveOutputs:  make(map[string]*liveOutput),
	}
	for name := range wf.Parameters {
		m.paramNames = append(m.paramNames, name)
	}
	sort.Strings(m.paramNames)
	m.updateParamInputs()
	return m
}

func (m model) Init() tea.Cmd {
	// Init returns no commands. If async loading is needed in the future, return a tea.Cmd here.
	return nil
}

type errMsg struct{ err error }
type saveTimerMsg struct{}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewports()
		m.updateParamInputWidths()

	case shellStdoutMsg:
		m.handleLiveOutput(msg.stepID, msg.line, true)
		if m.runner != nil {
			return m, m.runner.NextCmd()
		}

	case shellStderrMsg:
		m.handleLiveOutput(msg.stepID, msg.line, false)
		if m.runner != nil {
			return m, m.runner.NextCmd()
		}

	case shellDoneMsg:
		if m.currentStepID == msg.stepID {
			// Drain any remaining stdout/stderr before saving
			if m.runner != nil {
				stdoutLines, stderrLines := m.runner.Drain()
				liveOut := m.liveOutputs[msg.stepID]
				if liveOut == nil {
					liveOut = &liveOutput{}
					m.liveOutputs[msg.stepID] = liveOut
				}
				for _, line := range stdoutLines {
					liveOut.stdout = append(liveOut.stdout, line...)
				}
				for _, line := range stderrLines {
					liveOut.stderr = append(liveOut.stderr, line...)
				}
			}
			liveOut := m.liveOutputs[msg.stepID]
			if liveOut != nil {
				m.session.UpdateStepState(msg.stepID, StepState{
					Status:   msg.status,
					ExitCode: msg.exitCode,
					Stdout:   string(liveOut.stdout),
					Stderr:   string(liveOut.stderr),
				})
				// Sync the live buffers so refreshStdoutContent() renders the full output.
				m.stdoutBuffer = liveOut.stdout
				m.stderrBuffer = liveOut.stderr
			}
			delete(m.liveOutputs, msg.stepID)
		}
		m.runner = nil
		m.currentStepID = ""
		m.refreshStdoutContent()
		// For markdown output, scroll to top so the user sees the beginning
		if m.workflow != nil && m.cursor < len(m.workflow.Steps) && m.workflow.Steps[m.cursor].OutputType == OutputMarkdown {
			m.stdoutViewport.SetYOffset(0)
		}
		// Auto-run chain: if active and step succeeded, try to run the next auto-run step
		if m.autoRun {
			if msg.status == StatusSuccess {
				if m.workflow != nil && m.cursor < len(m.workflow.Steps)-1 {
					m.cursor++
					m.loadStepOutput()
					if m.canRun() && m.workflow.Steps[m.cursor].AutoRun {
						return m, m.runCurrentStep()
					}
				}
			}
			m.autoRun = false
		}
		return m, m.autoSave()

	case errMsg:
		m.saveErr = msg.err.Error()

	case saveTimerMsg:
		m.savePending = false
		if m.session != nil {
			if err := SaveSession(m.session); err != nil {
				return m, func() tea.Msg { return errMsg{err} }
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m model) handleKeyMsg(msg tea.KeyMsg) (model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.saveErr != "" {
		m.saveErr = ""
	}
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
		case "d":
			m.deleteSessionAtCursor()
			return m, m.autoSave()
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
		if m.runner != nil {
			m.runner.Stop()
		}
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
	case "R":
		if m.canRun() {
			m.autoRun = true
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
	// Overhead: titleBar + footer + infoPane content + frames for both left panes
	stepsContentH := max(stepsPaneMinHeight, m.height-titleBarHeight-footerHeight-infoPaneHeight-2*leftPaneStyle.GetVerticalFrameSize())

	leftContentRaw := m.renderStepListContent(leftContentW)

	stepsPane := leftPaneStyle.Width(leftW).Height(stepsContentH).Render(leftContentRaw)
	infoPane := leftPaneStyle.Width(leftW).Height(infoPaneHeight).Render(m.renderStepInfo(leftContentW))

	left := lipgloss.JoinVertical(lipgloss.Left, stepsPane, infoPane)

	rightContentW := max(2, rightW-paneFrameH)
	paramsContent := m.renderParamContent(rightContentW)
	stderrContent := m.stderrViewport.View()

	// For markdown output, bypass the viewport's broken MaxWidth truncation
	// and the pane's Width wrapping (both are not ANSI-aware in lipgloss).
	// Glamour already wraps at the correct width, so we render directly.
	var stdoutContent string
	if m.workflow != nil && m.cursor < len(m.workflow.Steps) && m.workflow.Steps[m.cursor].OutputType == OutputMarkdown && m.stdoutViewport.GetContent() != "" {
		stdoutContent = m.renderViewportContent()
	} else {
		stdoutContent = m.stdoutViewport.View()
	}

	params := paneStyle.Width(rightW).Render(
		paneTitleStyle.Render("Parameters") + "\n" + paramsContent)
	stdout := paneStyle.Width(rightW).Render(
		paneTitleStyle.Render("Stdout") + "\n" + stdoutContent)
	stderr := paneStyle.Width(rightW).Render(
		paneTitleStyle.Render("Stderr") + "\n" + stderrContent)

	right := lipgloss.JoinVertical(lipgloss.Left, params, stdout, stderr)

	// Render title bar
	titleBar := m.renderTitle()

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	footer := lipgloss.NewStyle().Height(1).Render(
		"↑/↓ nav  r run  R auto-run  d skip  tab params  s sessions  pgup/pgdn scroll  q quit",
	)

	all := lipgloss.JoinVertical(lipgloss.Left, titleBar, body, footer)
	v := tea.NewView(all)
	v.AltScreen = true
	return v
}

// Cursor returns the cursor position for the focused text input.
func (m model) Cursor() *tea.Cursor {
	if m.focusedParam < 0 || m.focusedParam >= len(m.paramNames) {
		return nil
	}
	name := m.paramNames[m.focusedParam]
	input := m.paramInputs[name]
	if !input.Focused() {
		return nil
	}
	c := input.Cursor()
	leftW := m.leftWidth()
	// X offset: left pane width + left border of the right pane
	// paneStyle has a symmetric border and no padding, so the left border width is half the horizontal frame.
	c.X += leftW + paneStyle.GetHorizontalFrameSize()/2
	const (
		paramsPaneBorderTop   = 1
		paramsPaneTitleHeight = 1
		paramLabelHeight      = 1
	)
	c.Y += titleBarHeight + paramsPaneBorderTop + paramsPaneTitleHeight + paramLabelHeight + m.focusedParam*paramBlockHeight
	return c
}

// --- Layout ---

func (m model) leftWidth() int {
	w := m.width / 2
	if w > leftPaneMaxWidth {
		w = leftPaneMaxWidth
	}
	return max(w, leftPaneMinWidth)
}

func (m model) rightWidth() int {
	return max(m.width-m.leftWidth(), rightPaneMinWidth)
}

func (m model) paramLines() int {
	if len(m.paramNames) == 0 {
		return 1
	}
	return len(m.paramNames) * paramBlockHeight
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
// While a step is running we render raw stdout/stderr so the user sees live
// output. After it finishes, we render markdown via glamour.
func (m *model) refreshStdoutContent() {
	paneFrameH := paneStyle.GetHorizontalFrameSize()
	normalWidth := max(2, m.rightWidth()-paneFrameH)
	stdoutStr := string(m.stdoutBuffer)

	if m.workflow == nil || m.cursor >= len(m.workflow.Steps) {
		m.stdoutViewport.SetWidth(normalWidth)
		m.stdoutViewport.SetContent(stdoutStr)
		m.stderrViewport.SetContent(string(m.stderrBuffer))
		return
	}

	step := m.workflow.Steps[m.cursor]
	isRunningThisStep := m.currentStepID == step.ID

	if !isRunningThisStep && step.OutputType == OutputMarkdown && stdoutStr != "" {
		rendered, err := m.renderMarkdown(stdoutStr, normalWidth)
		if err == nil {
			stdoutStr = rendered
		}
		m.stdoutViewport.SetContent(stdoutStr)
		m.stderrViewport.SetContent(string(m.stderrBuffer))
		m.mdViewportLines = strings.Split(stdoutStr, "\n")
		return
	}

	// Normal width for non-markdown content or while a step is running
	m.stdoutViewport.SetWidth(normalWidth)
	m.stdoutViewport.SetContent(stdoutStr)
	m.stderrViewport.SetContent(string(m.stderrBuffer))
	m.mdViewportLines = nil
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
			style = style.Copy().Background(lipgloss.Color(cursorBgColor)).Bold(true)
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

	descLine := lipgloss.NewStyle().Render(desc)
	runLine := lipgloss.NewStyle().Foreground(lipgloss.Color(lastRunFgColor)).Render("Last run: " + lastRun)
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
	if step.AutoRun {
		return "⏵"
	}
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
		// Format the datetime for display: 2006-01-02T15:04:05.000 -> 2006-01-02 15:04:05
		displayName := formatSessionNameForDisplay(sess.Name)
		line := fmt.Sprintf("%s%s (%s)", cursor, displayName, statusStyle.Render(status))
		lines = append(lines, line)
	}
	if len(m.sessionList) == 0 {
		lines = append(lines, "  (none)")
	}
	lines = append(lines, "", "enter pick  n new  d delete  q/esc close")

	modalW := min(modalMaxWidth, m.width-4)
	modalH := min(m.height-4, len(lines)+leftPaneStyle.GetVerticalFrameSize())
	contentW := max(2, modalW-leftPaneStyle.GetHorizontalFrameSize())
	contentH := max(1, modalH-leftPaneStyle.GetVerticalFrameSize())
	content := lipgloss.NewStyle().MaxWidth(contentW).MaxHeight(contentH).Render(strings.Join(lines, "\n"))
	overlay := leftPaneStyle.Width(contentW).Height(contentH).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
}

// deleteSessionAtCursor removes the highlighted session from disk and the list.
// If the deleted session was the current one, it switches to another session or creates a new one.
func (m *model) deleteSessionAtCursor() {
	if m.sessionList == nil || m.sessionCursor < 0 || m.sessionCursor >= len(m.sessionList) {
		return
	}
	sess := m.sessionList[m.sessionCursor]
	path := SessionPath(sess.WorkflowName, sess.Cwd, sess.Name)
	_ = os.Remove(path)

	wasCurrent := m.session != nil && m.session.Name == sess.Name

	m.sessionList = append(m.sessionList[:m.sessionCursor], m.sessionList[m.sessionCursor+1:]...)
	if m.sessionCursor >= len(m.sessionList) {
		m.sessionCursor = max(0, len(m.sessionList)-1)
	}

	if wasCurrent {
		if len(m.sessionList) > 0 {
			m.session = m.sessionList[m.sessionCursor]
			m.cursor = 0
			m.updateParamInputs()
			m.loadStepOutput()
		} else {
			m.session = NewSession(m.workflow, m.session.Cwd)
			m.cursor = 0
			m.updateParamInputs()
			m.stdoutBuffer = nil
			m.stderrBuffer = nil
			m.stdoutViewport.SetContent("")
			m.stderrViewport.SetContent("")
		}
	}
}

func (m model) renderSkipConfirm() string {
	if m.workflow == nil || m.cursor < 0 || m.cursor >= len(m.workflow.Steps) {
		return ""
	}
	step := m.workflow.Steps[m.cursor]
	var lines []string
	lines = append(lines, paneTitleStyle.Render("Skip Step"), "")
	lines = append(lines, fmt.Sprintf("Skip %q? (y/n)", step.Name))

	modalW := min(modalMaxWidth, m.width-4)
	modalH := min(m.height-4, len(lines)+leftPaneStyle.GetVerticalFrameSize())
	contentW := max(2, modalW-leftPaneStyle.GetHorizontalFrameSize())
	contentH := max(1, modalH-leftPaneStyle.GetVerticalFrameSize())
	content := lipgloss.NewStyle().MaxWidth(contentW).MaxHeight(contentH).Render(strings.Join(lines, "\n"))
	overlay := leftPaneStyle.Width(contentW).Height(contentH).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
}

// --- Logic ---

func (m *model) skipCurrentStep() {
	if m.workflow == nil || m.session == nil || m.cursor < 0 || m.cursor >= len(m.workflow.Steps) {
		return
	}
	step := m.workflow.Steps[m.cursor]
	m.session.UpdateStepState(step.ID, StepState{Status: StatusSkipped})
}

// handleLiveOutput appends a line to the live output buffer for a running step.
func (m *model) handleLiveOutput(stepID string, line string, isStdout bool) {
	if m.currentStepID != stepID {
		return
	}
	liveOut := m.liveOutputs[stepID]
	if liveOut == nil {
		liveOut = &liveOutput{}
		m.liveOutputs[stepID] = liveOut
	}
	if isStdout {
		liveOut.stdout = append(liveOut.stdout, line...)
	} else {
		liveOut.stderr = append(liveOut.stderr, line...)
	}
	if m.workflow != nil && m.cursor < len(m.workflow.Steps) && m.workflow.Steps[m.cursor].ID == stepID {
		if isStdout {
			m.stdoutBuffer = liveOut.stdout
			m.stdoutViewport.SetContent(string(m.stdoutBuffer))
			m.stdoutViewport.GotoBottom()
		} else {
			m.stderrBuffer = liveOut.stderr
			m.stderrViewport.SetContent(string(m.stderrBuffer))
			m.stderrViewport.GotoBottom()
		}
	}
}

// loadStepOutput populates the stdout/stderr buffers from the currently selected step.
// If the step is currently running, it loads from the live output buffer instead of
// the persisted session state so the user can navigate away and back without losing
// the live stream.
func (m *model) loadStepOutput() {
	if m.workflow == nil || m.session == nil || m.cursor >= len(m.workflow.Steps) {
		m.stdoutBuffer = nil
		m.stderrBuffer = nil
		m.stdoutViewport.SetContent("")
		m.stderrViewport.SetContent("")
		return
	}
	step := m.workflow.Steps[m.cursor]
	if m.currentStepID == step.ID {
		if liveOut, ok := m.liveOutputs[step.ID]; ok && liveOut != nil {
			m.stdoutBuffer = liveOut.stdout
			m.stderrBuffer = liveOut.stderr
		} else {
			m.stdoutBuffer = nil
			m.stderrBuffer = nil
		}
	} else {
		state := m.session.StepStates[step.ID]
		m.stdoutBuffer = []byte(state.Stdout)
		m.stderrBuffer = []byte(state.Stderr)
		// Backward compat: if new fields are empty, try old Output field
		if m.stdoutBuffer == nil && m.stderrBuffer == nil && state.Output != "" {
			out, stderr := state.GetOutput()
			m.stdoutBuffer = []byte(out)
			m.stderrBuffer = []byte(stderr)
		}
	}
	m.refreshStdoutContent()
	// For markdown, scroll to top so the beginning of the document is visible
	if step.OutputType == OutputMarkdown {
		m.stdoutViewport.SetYOffset(0)
	}
}

// renderMarkdown renders markdown content via glamour.
func (m *model) renderMarkdown(content string, width int) (string, error) {
	if m.mdRendererCache == nil {
		m.mdRendererCache = make(map[int]*glamour.TermRenderer)
	}
	renderer, ok := m.mdRendererCache[width]
	if !ok || renderer == nil {
		var err error
		renderer, err = glamour.NewTermRenderer(
			glamour.WithWordWrap(width),
			glamour.WithStandardStyle("dark"),
		)
		if err != nil {
			return "", err
		}
		m.mdRendererCache[width] = renderer
	}
	return renderer.Render(content)
}

// renderViewportContent returns the visible lines of the stdout viewport without
// applying the viewport's MaxWidth truncation.
//
// We bypass viewport.View() for markdown because lipgloss's MaxWidth
// truncation is not ANSI-aware; glamour already word-wraps at the correct
// width, so we manually slice the visible lines to avoid stripping ANSI codes.
func (m model) renderViewportContent() string {
	var lines []string
	if m.mdViewportLines != nil {
		lines = m.mdViewportLines
	} else {
		lines = strings.Split(m.stdoutViewport.GetContent(), "\n")
	}
	yOffset := m.stdoutViewport.YOffset()
	height := m.stdoutViewport.Height()
	top := max(0, yOffset)
	bottom := min(len(lines), top+height)
	if top >= len(lines) {
		return ""
	}
	visible := lines[top:bottom]
	// Pad to viewport height so the pane stays the same size regardless of content length.
	for len(visible) < height {
		visible = append(visible, "")
	}
	return strings.Join(visible, "\n")
}

// renderTitle returns the title bar showing workflow name, description, and session name.
// formatSessionNameForDisplay converts a raw session name (e.g. 2006-01-02T15:04:05.000)
// into a human-readable datetime (e.g. 2006-01-02 15:04:05).
func formatSessionNameForDisplay(name string) string {
	displayName := strings.Replace(name, "T", " ", 1)
	if idx := strings.LastIndex(displayName, "."); idx > 0 {
		displayName = displayName[:idx]
	}
	return displayName
}

func (m model) renderTitle() string {
	if m.workflow == nil || m.session == nil {
		return ""
	}
	wfName := m.workflow.Name
	wfDesc := m.workflow.Description
	sessionName := formatSessionNameForDisplay(m.session.Name)

	var parts []string
	parts = append(parts, titleStyle.Render(wfName))
	if wfDesc != "" {
		parts = append(parts, sessionStyle.Render("—"))
		parts = append(parts, sessionStyle.Render(wfDesc))
	}
	parts = append(parts, sessionStyle.Render("["+sessionName+"]"))

	if m.saveErr != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		parts = append(parts, errStyle.Render("⚠ "+m.saveErr))
	}

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
	if m.workflow == nil || m.session == nil || m.cursor < 0 || m.cursor >= len(m.workflow.Steps) {
		return false
	}
	step := m.workflow.Steps[m.cursor]
	state := m.session.StepStates[step.ID]
	return state.Status == StatusPending || state.Status == StatusFailed
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
	if m.session == nil || m.savePending {
		return nil
	}
	m.savePending = true
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return saveTimerMsg{}
	})
}

func (m *model) runCurrentStep() tea.Cmd {
	if m.workflow == nil || m.session == nil {
		return nil
	}
	step := m.workflow.Steps[m.cursor]
	scriptPath := ResolveScriptPath(m.workflowDir, step.Script)
	info, err := os.Stat(scriptPath)
	if err != nil {
		m.stderrBuffer = append(m.stderrBuffer, fmt.Sprintf("Script not found: %s\n", scriptPath)...)
		m.stderrViewport.SetContent(string(m.stderrBuffer))
		m.stderrViewport.GotoBottom()
		return nil
	}
	if info.Mode()&0111 == 0 {
		m.stderrBuffer = append(m.stderrBuffer, fmt.Sprintf("Script is not executable: %s\n", scriptPath)...)
		m.stderrViewport.SetContent(string(m.stderrBuffer))
		m.stderrViewport.GotoBottom()
		return nil
	}
	m.session.UpdateStepState(step.ID, StepState{Status: StatusRunning})
	m.stdoutBuffer = nil
	m.stderrBuffer = nil
	m.stdoutViewport.SetContent("")
	m.stderrViewport.SetContent("")
	m.liveOutputs[step.ID] = &liveOutput{}
	m.currentStepID = step.ID

	params := buildParams(step, m)
	m.runner = newStepRunner(step, m.workflowDir, scriptPath, params)
	return tea.Batch(m.autoSave(), m.runner.NextCmd())
}
