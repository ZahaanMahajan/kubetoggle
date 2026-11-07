package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zahaanmahajan/kubetoggle/internal/audit"
	"github.com/zahaanmahajan/kubetoggle/internal/scale"
	"github.com/zahaanmahajan/kubetoggle/internal/workload"

	k8sclient "k8s.io/client-go/kubernetes"
)

type Model struct {
	deployments []workload.Deployment
	cursor      int
	width       int
	height      int

	client  k8sclient.Interface
	manager *scale.Manager

	busy       bool
	message    string
	confirming bool
	pending    *workload.Deployment
	lastImpact *scale.Plan

	preview     *scale.Plan
	previewBusy bool
}

type scaleResultMsg struct {
	deployment workload.Deployment
	plan       *scale.Plan
	err        error
}

type previewResultMsg struct {
	deployment workload.Deployment
	plan       *scale.Plan
	err        error
}

type workloadsLoadedMsg struct {
	deployments []workload.Deployment
	err         error
}

var (
	// Theme
	colorPrimary   = lipgloss.Color("#A78BFA")
	colorSecondary = lipgloss.Color("#94A3B8")
	colorMuted     = lipgloss.Color("#64748B")
	colorSurface   = lipgloss.Color("#1E293B")
	colorBorder    = lipgloss.Color("#334155")
	colorText      = lipgloss.Color("#E2E8F0")
	colorGreen     = lipgloss.Color("#4ADE80")
	colorRed       = lipgloss.Color("#FB7185")
	colorYellow    = lipgloss.Color("#FBBF24")
	colorWhite     = lipgloss.Color("#FFFFFF")

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite)

	logoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorSecondary)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			Background(lipgloss.Color("#373064")).
			Padding(0, 1)

	normalStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1)

	groupStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSecondary).
			MarginTop(1)

	onStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorGreen)

	offStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorRed)

	warningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorYellow)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorText)

	keyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorSecondary).
			MarginTop(1)

	messageStyle = lipgloss.NewStyle().
			Foreground(colorGreen)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed)

	confirmationStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(colorPrimary).
				Padding(1, 2)
)

func NewModel(
	deployments []workload.Deployment,
	client k8sclient.Interface,
	auditor audit.Sink,
) Model {
	return Model{
		deployments: deployments,
		client:      client,
		manager:     scale.NewManager(client, auditor),
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case scaleResultMsg:
		m.busy = true

		if msg.err == nil {
			m.lastImpact = msg.plan
		}

		if msg.err != nil {
			m.message = errorStyle.Render(
				fmt.Sprintf("✗ Operation failed: %v", msg.err),
			)
		} else if msg.deployment.Replicas == 0 {
			m.message = messageStyle.Render(
				fmt.Sprintf(
					"✓ Enabled %s/%s",
					msg.deployment.Namespace,
					msg.deployment.Name,
				),
			)
		} else {
			m.message = messageStyle.Render(
				fmt.Sprintf(
					"✓ Disabled %s/%s",
					msg.deployment.Namespace,
					msg.deployment.Name,
				),
			)
		}

		return m, refreshDeploymentsCmd(m.client)

	case previewResultMsg:
		m.previewBusy = false

		if !m.confirming || m.pending == nil ||
			m.pending.Namespace != msg.deployment.Namespace ||
			m.pending.Name != msg.deployment.Name {
			return m, nil
		}

		if msg.err != nil {
			m.message = errorStyle.Render(
				fmt.Sprintf("✗ Preview failed: %v", msg.err),
			)
			m.confirming = false
			m.pending = nil
			m.preview = nil
			return m, nil
		}

		m.preview = msg.plan
		return m, nil

	case workloadsLoadedMsg:
		m.busy = false

		if m.message == "Refreshing workloads..." {
			m.message = ""
		}

		if msg.err != nil {
			m.message = errorStyle.Render(
				fmt.Sprintf("✗ Refresh failed: %v", msg.err),
			)
			return m, nil
		}

		m.deployments = msg.deployments

		if len(m.deployments) == 0 {
			m.cursor = 0
		} else if m.cursor >= len(m.deployments) {
			m.cursor = len(m.deployments) - 1
		}

		return m, nil

	case tea.KeyMsg:
		if m.confirming {
			switch msg.String() {
			case "y":
				if m.previewBusy || m.preview == nil {
					return m, nil
				}
				selected := *m.pending
				m.confirming = false
				m.pending = nil
				m.preview = nil
				m.previewBusy = false
				m.busy = true
				m.message = "Scaling Deployment..."

				return m, scaleDeploymentCmd(m.manager, selected)

			case "n", "esc":
				m.confirming = false
				m.pending = nil
				m.preview = nil
				m.previewBusy = false
				m.message = "Operation cancelled"
			}

			return m, nil
		}

		if m.busy {
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.deployments)-1 {
				m.cursor++
			}

		case " ":
			if len(m.deployments) == 0 {
				return m, nil
			}

			selected := m.deployments[m.cursor]
			m.pending = &selected
			m.preview = nil
			m.previewBusy = true
			m.confirming = true

			return m, previewDeploymentCmd(m.manager, selected)

		case "r":
			m.busy = true
			m.message = "Refreshing workloads..."

			return m, refreshDeploymentsCmd(m.client)
		}
	}

	return m, nil
}

func (m Model) View() string {
	width := m.width
	height := m.height

	if width <= 0 {
		width = 110
	}

	if height <= 0 {
		height = 32
	}

	header := m.renderHeader(width)
	overview := m.renderResourceOverview(width)
	footer := m.renderFooter(width)

	bodyHeight := height -
		lipgloss.Height(header) -
		lipgloss.Height(overview) -
		lipgloss.Height(footer) - 3

	if bodyHeight < 8 {
		bodyHeight = 8
	}

	var body string

	if width >= 100 {
		leftWidth := width * 45 / 100
		rightWidth := width - leftWidth - 1

		left := m.renderWorkloadPanel(leftWidth, bodyHeight)
		right := m.renderDetailsPanel(rightWidth, bodyHeight)

		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			left,
			right,
		)
	} else {
		left := m.renderWorkloadPanel(width, bodyHeight)
		right := m.renderDetailsPanel(width, bodyHeight)

		body = lipgloss.JoinVertical(
			lipgloss.Left,
			left,
			right,
		)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		overview,
		body,
		footer,
	)
}

func (m Model) renderHeader(width int) string {
	brand := logoStyle.Render("◆ KUBETOGGLE")

	description := subtitleStyle.Render(
		"Kubernetes workload control",
	)

	status := onStyle.Render("● CLIENT READY")

	title := lipgloss.JoinHorizontal(
		lipgloss.Center,
		brand,
		"  ",
		description,
	)

	top := lipgloss.JoinHorizontal(
		lipgloss.Center,
		title,
		strings.Repeat(" ", max(1, width-lipgloss.Width(title)-lipgloss.Width(status)-4)),
		status,
	)

	stats := subtitleStyle.Render(
		fmt.Sprintf(
			"%d applications  ·  %d workloads  ·  %d running  ·  %d stopped",
			m.applicationCount(),
			len(m.deployments),
			m.runningCount(),
			m.stoppedCount(),
		),
	)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		top,
		stats,
		"",
	)
}

func (m Model) renderResourceOverview(width int) string {
	summary := workload.Summarize(m.deployments)

	var b strings.Builder

	b.WriteString(panelTitleStyle.Render(
		"RESOURCE OVERVIEW · TOGGLEABLE WORKLOADS",
	))
	b.WriteString("\n")

	fmt.Fprintf(
		&b,
		"%s  %d    %s  %d    %s  %d    %s  %d\n",
		mutedStyle.Render("Total:"),
		summary.TotalWorkloads,
		onStyle.Render("Running:"),
		summary.RunningWorkloads,
		offStyle.Render("Stopped:"),
		summary.StoppedWorkloads,
		mutedStyle.Render("Replicas:"),
		summary.RunningReplicas,
	)

	current := summary.RunningResources
	recoverable := summary.RecoverableResources

	b.WriteString("\n")
	b.WriteString(panelTitleStyle.Render("CURRENT RESERVATIONS"))
	b.WriteString("\n")

	fmt.Fprintf(
		&b,
		"CPU requests: %s  ·  CPU limits: %s\n",
		formatCPU(current.CPURequestMilli),
		formatCPU(current.CPULimitMilli),
	)

	fmt.Fprintf(
		&b,
		"Memory requests: %s  ·  Memory limits: %s\n",
		formatMemory(current.MemoryRequestBytes),
		formatMemory(current.MemoryLimitBytes),
	)

	b.WriteString("\n")
	b.WriteString(panelTitleStyle.Render("POTENTIALLY RECOVERABLE"))
	b.WriteString("\n")

	fmt.Fprintf(
		&b,
		"CPU requests: %s  ·  CPU limits: %s\n",
		formatCPU(recoverable.CPURequestMilli),
		formatCPU(recoverable.CPULimitMilli),
	)

	fmt.Fprintf(
		&b,
		"Memory requests: %s  ·  Memory limits: %s",
		formatMemory(recoverable.MemoryRequestBytes),
		formatMemory(recoverable.MemoryLimitBytes),
	)

	return panelStyle.
		Width(max(1, width-2)).
		Render(b.String())
}

func (m Model) renderWorkloadPanel(width, height int) string {
	title := panelTitleStyle.Render("WORKLOADS")

	if len(m.deployments) == 0 {
		content := lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"",
			mutedStyle.Render("No toggleable Deployments found."),
			"",
			subtitleStyle.Render("Press r to refresh."),
		)

		return panelStyle.Width(max(1, width-2)).
			Height(max(1, height-2)).
			Render(content)
	}

	var b strings.Builder

	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("APPLICATION / DEPLOYMENT"))
	b.WriteString("\n\n")

	availableLines := max(1, height-9)

	start := 0
	if m.cursor >= availableLines {
		start = m.cursor - availableLines + 1
	}

	end := min(len(m.deployments), start+availableLines)

	currentApplication := ""

	for i := start; i < end; i++ {
		deployment := m.deployments[i]

		application := deployment.Application
		if application == "" {
			application = "unmanaged"
		}

		if application != currentApplication {
			currentApplication = application

			fmt.Fprintf(
				&b,
				"%s\n",
				groupStyle.Render("▾ "+application),
			)
		}

		state := offStyle.Render("● OFF")
		if deployment.Replicas > 0 {
			state = onStyle.Render("● ON ")
		}

		name := deployment.Name
		if len(name) > 22 {
			name = name[:19] + "..."
		}

		replicas := fmt.Sprintf("%d replicas", deployment.Replicas)

		line := fmt.Sprintf(
			"  %-24s %-8s %s",
			name,
			state,
			mutedStyle.Render(replicas),
		)

		if i == m.cursor {
			line = selectedStyle.Width(max(1, width-8)).Render(
				fmt.Sprintf("› %s", strings.TrimSpace(line)),
			)
		} else {
			line = normalStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	if start > 0 || end < len(m.deployments) {
		b.WriteString("\n")
		b.WriteString(
			mutedStyle.Render(
				fmt.Sprintf(
					"Showing %d-%d of %d workloads",
					start+1,
					end,
					len(m.deployments),
				),
			),
		)
	}

	content := b.String()

	return panelStyle.Width(max(1, width-2)).
		Height(max(1, height-2)).
		Render(content)
}

func (m Model) renderDetailsPanel(width, height int) string {
	if m.confirming && m.pending != nil {
		return m.renderConfirmationPanel(width, height)
	}

	var left, right strings.Builder

	left.WriteString(panelTitleStyle.Render("DEPLOYMENT DETAILS"))
	left.WriteString("\n\n")

	if len(m.deployments) == 0 {
		left.WriteString(mutedStyle.Render("Select a workload to inspect."))

		return panelStyle.
			Width(max(1, width-2)).
			Height(max(1, height-2)).
			Render(left.String())
	}

	deployment := m.deployments[m.cursor]

	state := offStyle.Render("● OFFLINE")
	if deployment.Replicas > 0 {
		state = onStyle.Render("● RUNNING")
	}

	left.WriteString(headerStyle.Render(deployment.Name))
	left.WriteString("\n")
	left.WriteString(state)
	left.WriteString("\n\n")

	m.renderDetailRow(&left, "APPLICATION", deployment.Application)
	m.renderDetailRow(&left, "NAMESPACE", deployment.Namespace)
	m.renderDetailRow(
		&left,
		"DESIRED REPLICAS",
		fmt.Sprintf("%d", deployment.Replicas),
	)

	left.WriteString(panelTitleStyle.Render("PERSISTENT STORAGE"))
	left.WriteString("\n")

	if len(deployment.PVCs) == 0 {
		left.WriteString(mutedStyle.Render("No PVCs referenced"))
		left.WriteString("\n")
	} else {
		for _, claim := range deployment.PVCs {
			fmt.Fprintf(&left, "◇ %s\n", valueStyle.Render(claim))
		}
	}

	left.WriteString("\n")
	left.WriteString(panelTitleStyle.Render("AVAILABLE ACTION"))
	left.WriteString("\n")

	if deployment.Replicas == 0 {
		left.WriteString(" ")
		left.WriteString(keyStyle.Render("[SPACE]"))
		left.WriteString(" Restore workload\n")
	} else {
		left.WriteString(" ")
		left.WriteString(keyStyle.Render("[SPACE]"))
		left.WriteString(" Disable workload\n")
	}

	right.WriteString(panelTitleStyle.Render("RESOURCE RESERVATIONS"))
	right.WriteString("\n\n")

	current := deployment.Resources
	replicas := int64(deployment.Replicas)

	current.CPURequestMilli *= replicas
	current.MemoryRequestBytes *= replicas
	current.CPULimitMilli *= replicas
	current.MemoryLimitBytes *= replicas

	right.WriteString(panelTitleStyle.Render("CURRENT"))
	right.WriteString("\n")

	right.WriteString(renderResourceValues(current))

	if plan := m.lastImpact; plan != nil &&
		plan.Namespace == deployment.Namespace &&
		plan.Deployment == deployment.Name {

		right.WriteString("\n")
		right.WriteString(panelTitleStyle.Render("LAST OPERATION"))
		right.WriteString("\n")

		right.WriteString(m.renderResourceDelta(plan))
	}

	if m.busy {
		right.WriteString("\n")
		right.WriteString(warningStyle.Render("◌ Working..."))
	}

	if m.message != "" {
		right.WriteString("\n\n")
		right.WriteString(m.message)
	}

	contentWidth := max(1, width-8)
	leftWidth := max(1, contentWidth*42/100)
	rightWidth := max(1, contentWidth-leftWidth-2)

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).Render(left.String()),
		"  ",
		lipgloss.NewStyle().Width(rightWidth).Render(right.String()),
	)

	return panelStyle.
		Width(max(1, width-2)).
		Height(max(1, height-2)).
		Render(content)
}

func (m Model) renderConfirmationPanel(width, height int) string {
	deployment := m.pending

	action := "DISABLE"
	actionColor := colorRed

	if deployment.Replicas == 0 {
		action = "ENABLE"
		actionColor = colorGreen
	}

	actionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(actionColor)

	var left, right strings.Builder

	left.WriteString(panelTitleStyle.Render("CONFIRM ACTION"))
	left.WriteString("\n\n")

	left.WriteString(actionStyle.Render("◆ " + action + " WORKLOAD"))
	left.WriteString("\n\n")

	left.WriteString(headerStyle.Render(deployment.Name))
	left.WriteString("\n")
	left.WriteString(mutedStyle.Render(deployment.Namespace))
	left.WriteString("\n\n")

	fmt.Fprintf(
		&left,
		"%s %d → %d\n\n",
		mutedStyle.Render("Replicas:"),
		deployment.Replicas,
		func() int32 {
			if deployment.Replicas == 0 {
				return 1
			}
			return 0
		}(),
	)

	if deployment.Replicas > 0 && len(deployment.PVCs) > 0 {
		left.WriteString(warningStyle.Render("⚠ PERSISTENT STORAGE WARNING"))
		left.WriteString("\n")
		left.WriteString("Scaling to zero stops its Pods.\n")
		left.WriteString("PVCs and PVs will not be deleted.\n\n")
	}

	left.WriteString("Are you sure you want to continue?\n\n")

	if m.previewBusy || m.preview == nil {
		left.WriteString(mutedStyle.Render("Waiting for resource preview..."))
	} else {
		left.WriteString(keyStyle.Render("[Y]"))
		left.WriteString(" Confirm    ")
		left.WriteString(keyStyle.Render("[N]"))
		left.WriteString(" Cancel")
	}

	right.WriteString(panelTitleStyle.Render("RESOURCE IMPACT PREVIEW"))
	right.WriteString("\n\n")

	if m.previewBusy {
		right.WriteString(warningStyle.Render("◌ Calculating resource impact..."))
	} else if m.preview != nil {
		right.WriteString(m.renderResourceDelta(m.preview))
	} else {
		right.WriteString(errorStyle.Render("Resource preview unavailable."))
	}

	contentWidth := max(1, width-16)
	leftWidth := max(1, contentWidth*42/100)
	rightWidth := max(1, contentWidth-leftWidth-2)

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).Render(left.String()),
		"  ",
		lipgloss.NewStyle().Width(rightWidth).Render(right.String()),
	)

	return confirmationStyle.
		Width(max(1, width-8)).
		Height(max(1, height-4)).
		Render(content)
}

func renderResourceValues(resources workload.Resources) string {
	var b strings.Builder

	fmt.Fprintf(
		&b,
		"CPU request     %s\n",
		formatCPU(resources.CPURequestMilli),
	)

	fmt.Fprintf(
		&b,
		"Memory request  %s\n",
		formatMemory(resources.MemoryRequestBytes),
	)

	fmt.Fprintf(
		&b,
		"CPU limit       %s\n",
		formatCPU(resources.CPULimitMilli),
	)

	fmt.Fprintf(
		&b,
		"Memory limit    %s\n",
		formatMemory(resources.MemoryLimitBytes),
	)

	return b.String()
}

func (m Model) renderResourceDelta(plan *scale.Plan) string {
	var b strings.Builder

	delta := int64(plan.TargetReplicas) - int64(plan.CurrentReplicas)

	fmt.Fprintf(
		&b,
		"%s\n\n",
		mutedStyle.Render(
			fmt.Sprintf(
				"Replicas: %d → %d",
				plan.CurrentReplicas,
				plan.TargetReplicas,
			),
		),
	)

	if delta == 0 {
		b.WriteString(mutedStyle.Render("No resource reservation change.\n"))
		return b.String()
	}

	action := "RESOURCES RELEASED"
	actionStyle := offStyle

	if delta > 0 {
		action = "RESOURCES REQUESTED"
		actionStyle = onStyle
	}

	b.WriteString(actionStyle.Render(action))
	b.WriteString("\n\n")

	resources := plan.Resources

	impact := workload.Resources{
		CPURequestMilli:    absInt64(resources.CPURequestMilli * delta),
		MemoryRequestBytes: absInt64(resources.MemoryRequestBytes * delta),
		CPULimitMilli:      absInt64(resources.CPULimitMilli * delta),
		MemoryLimitBytes:   absInt64(resources.MemoryLimitBytes * delta),
	}

	b.WriteString(renderResourceValues(impact))

	return b.String()
}

func (m Model) renderDetailRow(b *strings.Builder, label, value string) {
	if value == "" {
		value = "—"
	}

	fmt.Fprintf(
		b,
		"%s\n%s\n\n",
		mutedStyle.Render(label),
		valueStyle.Render(value),
	)
}

func (m Model) renderFooter(width int) string {
	var b strings.Builder

	if m.busy {
		b.WriteString(warningStyle.Render("◌ PROCESSING"))
		b.WriteString("  ")
	} else {
		b.WriteString(keyStyle.Render("↑/↓"))
		b.WriteString(" Navigate   ")

		b.WriteString(keyStyle.Render("SPACE"))
		b.WriteString(" Toggle   ")

		b.WriteString(keyStyle.Render("R"))
		b.WriteString(" Refresh   ")

		b.WriteString(keyStyle.Render("Q"))
		b.WriteString(" Quit")
	}

	if m.confirming {
		return footerStyle.Render(
			"  " + keyStyle.Render("Y") + " Confirm   " +
				keyStyle.Render("N / ESC") + " Cancel",
		)
	}

	return footerStyle.Render(
		lipgloss.NewStyle().MaxWidth(width).Render(b.String()),
	)
}

func (m Model) applicationCount() int {
	applications := make(map[string]struct{})

	for _, deployment := range m.deployments {
		application := deployment.Application
		if application == "" {
			application = "unmanaged"
		}

		applications[application] = struct{}{}
	}

	return len(applications)
}

func (m Model) runningCount() int {
	count := 0

	for _, deployment := range m.deployments {
		if deployment.Replicas > 0 {
			count++
		}
	}

	return count
}

func (m Model) stoppedCount() int {
	return len(m.deployments) - m.runningCount()
}

func scaleDeploymentCmd(
	manager *scale.Manager,
	deployment workload.Deployment,
) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		enable := deployment.Replicas == 0

		plan, err := manager.Preview(
			ctx,
			deployment.Namespace,
			deployment.Name,
			enable,
		)
		if err != nil {
			return scaleResultMsg{
				deployment: deployment,
				err:        err,
			}
		}

		if enable {
			err = manager.On(ctx, deployment.Namespace, deployment.Name)
		} else {
			err = manager.Off(ctx, deployment.Namespace, deployment.Name)
		}

		return scaleResultMsg{
			deployment: deployment,
			plan:       plan,
			err:        err,
		}
	}
}

func refreshDeploymentsCmd(client k8sclient.Interface) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		deployments, err := workload.Discover(ctx, client)

		return workloadsLoadedMsg{
			deployments: deployments,
			err:         err,
		}
	}
}

func formatCPU(milli int64) string {
	if milli == 0 {
		return "0"
	}

	if milli >= 1000 && milli%1000 == 0 {
		return fmt.Sprintf("%d cores", milli/1000)
	}

	return fmt.Sprintf("%dm", milli)
}

func formatMemory(bytes int64) string {
	if bytes == 0 {
		return "0"
	}

	const (
		miB = 1024 * 1024
		giB = 1024 * miB
	)

	if bytes >= giB {
		return fmt.Sprintf("%.2f GiB", float64(bytes)/float64(giB))
	}

	return fmt.Sprintf("%.0f MiB", float64(bytes)/float64(miB))
}

func (m Model) renderResourceImpact(b *strings.Builder, deployment workload.Deployment) {
	plan := m.lastImpact

	if plan == nil ||
		plan.Namespace != deployment.Namespace ||
		plan.Deployment != deployment.Name {
		return
	}

	b.WriteString("\n")
	b.WriteString(panelTitleStyle.Render("LAST RESOURCE IMPACT"))
	b.WriteString("\n")

	before := plan.Resources
	currentReplicas := int64(plan.CurrentReplicas)
	targetReplicas := int64(plan.TargetReplicas)

	cpuRequestDelta := before.CPURequestMilli * (targetReplicas - currentReplicas)
	memoryRequestDelta := before.MemoryRequestBytes * (targetReplicas - currentReplicas)
	cpuLimitDelta := before.CPULimitMilli * (targetReplicas - currentReplicas)
	memoryLimitDelta := before.MemoryLimitBytes * (targetReplicas - currentReplicas)

	if currentReplicas == targetReplicas {
		b.WriteString(mutedStyle.Render("No replica change; no resource reservation change."))
		b.WriteString("\n")
		return
	}

	action := "RELEASED"
	actionStyle := offStyle

	if targetReplicas > currentReplicas {
		action = "REQUESTED"
		actionStyle = onStyle
	}

	b.WriteString(
		mutedStyle.Render(
			fmt.Sprintf(
				"Replicas: %d → %d",
				plan.CurrentReplicas,
				plan.TargetReplicas,
			),
		),
	)
	b.WriteString("\n")
	b.WriteString(actionStyle.Render("Resources " + action))
	b.WriteString("\n\n")

	renderDelta := func(label, value string) {
		fmt.Fprintf(
			b,
			"  %-16s %s\n",
			mutedStyle.Render(label),
			valueStyle.Render(value),
		)
	}

	renderDelta("CPU request", formatCPU(absInt64(cpuRequestDelta)))
	renderDelta("Memory request", formatMemory(absInt64(memoryRequestDelta)))
	renderDelta("CPU limit", formatCPU(absInt64(cpuLimitDelta)))
	renderDelta("Memory limit", formatMemory(absInt64(memoryLimitDelta)))
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func previewDeploymentCmd(
	manager *scale.Manager,
	deployment workload.Deployment,
) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		enable := deployment.Replicas == 0

		plan, err := manager.Preview(
			ctx,
			deployment.Namespace,
			deployment.Name,
			enable,
		)

		return previewResultMsg{
			deployment: deployment,
			plan:       plan,
			err:        err,
		}
	}
}
