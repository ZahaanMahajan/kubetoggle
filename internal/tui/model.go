package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zahaanmahajan/kubetoggle/internal/scale"
	"github.com/zahaanmahajan/kubetoggle/internal/workload"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
}

type scaleResultMsg struct {
	deployment workload.Deployment
	err        error
}

type workloadsLoadedMsg struct {
	deployments []workload.Deployment
	err         error
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	messageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))
)

func NewModel(
	deployments []workload.Deployment,
	client k8sclient.Interface,
) Model {
	return Model{
		deployments: deployments,
		client:      client,
		manager:     scale.NewManager(client),
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

		if msg.err != nil {
			m.message = errorStyle.Render(
				fmt.Sprintf("Operation failed: %v", msg.err),
			)
		} else if msg.deployment.Replicas == 0 {
			m.message = messageStyle.Render(
				fmt.Sprintf("Enabled %s/%s", msg.deployment.Namespace, msg.deployment.Name),
			)
		} else {
			m.message = messageStyle.Render(
				fmt.Sprintf("Disabled %s/%s", msg.deployment.Namespace, msg.deployment.Name),
			)
		}

		return m, refreshDeploymentsCmd(m.client)

	case workloadsLoadedMsg:
		m.busy = false

		if m.message == "Refreshing workloads..." {
			m.message = ""
		}

		if msg.err != nil {
			m.message = errorStyle.Render(
				fmt.Sprintf("Refresh failed: %v", msg.err),
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
				selected := *m.pending
				m.confirming = false
				m.pending = nil
				m.busy = true
				m.message = "Scaling Deployment..."

				return m, scaleDeploymentCmd(m.manager, selected)

			case "n", "esc":
				m.confirming = false
				m.pending = nil
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
			m.confirming = true

		case "r":
			m.busy = true
			m.message = "Refreshing workloads..."

			return m, refreshDeploymentsCmd(m.client)
		}
	}

	return m, nil
}

func (m Model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("KubeToggle"))
	b.WriteString("\n")
	b.WriteString("Kubernetes workload manager\n")

	if len(m.deployments) == 0 {
		b.WriteString("\nNo toggleable Deployments found.\n")
	} else {
		currentApplication := ""

		for i, deployment := range m.deployments {
			application := deployment.Application
			if application == "" {
				application = "unmanaged"
			}

			if application != currentApplication {
				currentApplication = application
				fmt.Fprintf(&b, "\n[%s]\n", currentApplication)
			}

			prefix := "  "
			if i == m.cursor {
				prefix = "▶ "
			}

			state := "ON"
			if deployment.Replicas == 0 {
				state = "OFF"
			}

			line := fmt.Sprintf(
				"%s%-12s %-24s replicas=%d [%s]",
				prefix,
				deployment.Namespace,
				deployment.Name,
				deployment.Replicas,
				state,
			)

			if i == m.cursor {
				b.WriteString(selectedStyle.Render(line))
			} else {
				b.WriteString(normalStyle.Render(line))
			}

			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	if m.busy {
		b.WriteString("Working... ")
	}

	if m.confirming && m.pending != nil {
		action := "disable"
		if m.pending.Replicas == 0 {
			action = "enable"
		}

		fmt.Fprintf(
			&b,
			"\nConfirm: %s %s/%s?\n",
			action,
			m.pending.Namespace,
			m.pending.Name,
		)

		// Show persistent storage warning only when disabling.
		if m.pending.Replicas > 0 && len(m.pending.PVCs) > 0 {
			b.WriteString("\n")
			b.WriteString(
				"WARNING: This Deployment references persistent storage:\n",
			)
			b.WriteString("\n")

			for _, claim := range m.pending.PVCs {
				fmt.Fprintf(&b, "  - %s\n", claim)
			}

			b.WriteString(
				"Scaling to zero stops its Pods. PVCs and PVs will not be deleted.\n",
			)
		}

		b.WriteString("Press y to confirm or n/Esc to cancel.\n")
	}

	if m.message != "" {
		b.WriteString(m.message)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render(
		"↑/k: up  ↓/j: down  space: toggle  r: refresh  q: quit",
	))

	return b.String()
}

func scaleDeploymentCmd(
	manager *scale.Manager,
	deployment workload.Deployment,
) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var err error

		if deployment.Replicas == 0 {
			err = manager.On(ctx, deployment.Namespace, deployment.Name)
		} else {
			err = manager.Off(ctx, deployment.Namespace, deployment.Name)
		}

		return scaleResultMsg{
			deployment: deployment,
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
