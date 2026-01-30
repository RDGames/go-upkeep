package tui

import (
	"fmt"
	"go-upkeep/internal/db"
	"go-upkeep/internal/models"
	"go-upkeep/internal/monitor"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#D9DCCF", Dark: "#383838"})
	specialStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"})
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#F0E442", Dark: "#F0E442"})
	dangerStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#F25D94", Dark: "#F25D94"})
	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)

	activeTab   = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(lipgloss.Color("#7D56F4")).Foreground(lipgloss.Color("#7D56F4")).Bold(true).Padding(0, 1)
	inactiveTab = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.AdaptiveColor{Light: "#AAA", Dark: "#555"})

	colID     = lipgloss.NewStyle().Width(4)
	colURL    = lipgloss.NewStyle().Width(30)
	colStatus = lipgloss.NewStyle().Width(12)
	colSSL    = lipgloss.NewStyle().Width(15)
	colCode   = lipgloss.NewStyle().Width(6)
)

type alertItem struct {
	id          int
	name, aType string
}

func (i alertItem) Title() string { return i.name }
func (i alertItem) Description() string {
	if i.id == -1 {
		return "Set up a new notification channel now"
	}
	return fmt.Sprintf("ID: %d | Type: %s", i.id, i.aType)
}
func (i alertItem) FilterValue() string { return i.name }

type sessionState int

const (
	stateDashboard sessionState = iota
	stateLogs
	stateFormSite
	stateFormAlert
	stateSelectAlert
)

type Model struct {
	state      sessionState
	currentTab int

	cursor       int
	tableOffset  int
	maxTableRows int

	editID   int
	inputs   []textinput.Model
	focus    int
	errorMsg string

	logViewport  viewport.Model
	formViewport viewport.Model

	alertList             list.Model
	creatingAlertFromSite bool

	sites  []models.Site
	alerts []models.AlertConfig
}

func InitialModel() Model {
	vpLogs := viewport.New(100, 20)
	vpLogs.SetContent("Waiting for logs...")
	vpForm := viewport.New(100, 20)

	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select Alert Config"
	l.SetShowHelp(false)

	return Model{
		state:        stateDashboard,
		inputs:       []textinput.Model{},
		logViewport:  vpLogs,
		formViewport: vpForm,
		alertList:    l,
		maxTableRows: 5,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return t })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := 4
		footerHeight := 2
		m.maxTableRows = msg.Height - headerHeight - footerHeight - 3
		if m.maxTableRows < 1 {
			m.maxTableRows = 1
		}

		m.logViewport.Width = msg.Width
		m.logViewport.Height = msg.Height - 6
		
		m.formViewport.Width = msg.Width
		m.formViewport.Height = msg.Height - 3 
		
		m.alertList.SetSize(msg.Width, msg.Height-4)

	case time.Time:
		m.refreshData()
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return t })

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.state == stateSelectAlert {
			switch msg.String() {
			case "esc":
				m.state = stateFormSite
				m.updateFormContent()
				return m, nil
			case "enter":
				selectedItem := m.alertList.SelectedItem()
				if selectedItem != nil {
					itm := selectedItem.(alertItem)
					if itm.id == -1 {
						m.state = stateFormAlert
						m.initFormAlert()
						m.creatingAlertFromSite = true
						m.editID = 0
						m.formViewport.GotoTop()
						m.updateFormContent()
						return m, nil
					} else {
						m.inputs[2].SetValue(strconv.Itoa(itm.id))
						m.state = stateFormSite
						m.updateFormContent()
						return m, nil
					}
				}
			}
			m.alertList, cmd = m.alertList.Update(msg)
			return m, cmd
		}

		switch m.state {
		case stateDashboard, stateLogs:
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "tab":
				m.currentTab++
				if m.currentTab > 2 {
					m.currentTab = 0
				}
				m.cursor = 0
				m.tableOffset = 0
				if m.currentTab == 2 {
					m.state = stateLogs
				} else {
					m.state = stateDashboard
				}
			case "pgup", "pgdown":
				if m.state == stateLogs {
					m.logViewport, cmd = m.logViewport.Update(msg)
					return m, cmd
				}
			case "up", "k":
				if m.state == stateLogs {
					m.logViewport.LineUp(1)
				} else if m.cursor > 0 {
					m.cursor--
					if m.cursor < m.tableOffset {
						m.tableOffset = m.cursor
					}
				}
			case "down", "j":
				if m.state == stateLogs {
					m.logViewport.LineDown(1)
				} else {
					max := len(m.sites) - 1
					if m.currentTab == 1 {
						max = len(m.alerts) - 1
					}
					if m.cursor < max {
						m.cursor++
						if m.cursor >= m.tableOffset+m.maxTableRows {
							m.tableOffset++
						}
					}
				}
			case "n":
				if m.currentTab != 2 {
					m.editID = 0
					m.errorMsg = ""
					if m.currentTab == 1 {
						m.state = stateFormAlert
						m.initFormAlert()
					} else {
						m.state = stateFormSite
						m.initFormSite()
					}
					m.formViewport.GotoTop()
					m.updateFormContent()
					return m, nil
				}
			case "e", "enter":
				m.errorMsg = ""
				if m.currentTab == 1 && len(m.alerts) > 0 {
					target := m.alerts[m.cursor]
					m.editID = target.ID
					m.state = stateFormAlert
					m.initFormAlert()
					m.inputs[0].SetValue(target.Name)
					m.inputs[1].SetValue(target.Type)
					m.inputs[2].SetValue(target.WebhookURL)
					m.formViewport.GotoTop()
					m.updateFormContent()
					return m, nil
				} else if m.currentTab == 0 && len(m.sites) > 0 {
					target := m.sites[m.cursor]
					m.editID = target.ID
					m.state = stateFormSite
					m.initFormSite()
					m.inputs[0].SetValue(target.URL)
					m.inputs[1].SetValue(strconv.Itoa(target.Interval))
					m.inputs[2].SetValue(strconv.Itoa(target.AlertID))
					sslVal := "n"
					if target.CheckSSL {
						sslVal = "y"
					}
					m.inputs[3].SetValue(sslVal)
					m.inputs[4].SetValue(strconv.Itoa(target.ExpiryThreshold))
					m.formViewport.GotoTop()
					m.updateFormContent()
					return m, nil
				}
			case "d", "backspace":
				if m.currentTab == 1 && len(m.alerts) > 0 {
					db.DeleteAlert(m.alerts[m.cursor].ID)
					if m.cursor >= len(m.alerts)-1 && m.cursor > 0 {
						m.cursor--
					}
					if m.cursor < m.tableOffset {
						m.tableOffset = m.cursor
					}
					if m.tableOffset < 0 {
						m.tableOffset = 0
					}
					m.refreshData()
				} else if m.currentTab == 0 && len(m.sites) > 0 {
					id := m.sites[m.cursor].ID
					db.DeleteSite(id)
					monitor.RemoveSite(id)
					if m.cursor >= len(m.sites)-1 && m.cursor > 0 {
						m.cursor--
					}
					if m.cursor < m.tableOffset {
						m.tableOffset = m.cursor
					}
					if m.tableOffset < 0 {
						m.tableOffset = 0
					}
					m.refreshData()
				}
			}

		case stateFormSite, stateFormAlert:
			switch msg.String() {
			case "esc":
				if m.creatingAlertFromSite {
					m.creatingAlertFromSite = false
					m.state = stateFormSite
					m.initFormSite()
				} else {
					m.state = stateDashboard
				}
				m.updateFormContent()
				return m, nil

			case "pgup", "pgdown":
				m.formViewport, cmd = m.formViewport.Update(msg)
				return m, cmd

			case "tab", "shift+tab", "enter", "up", "down":
				s := msg.String()

				if m.state == stateFormSite && m.focus == 2 && s == "enter" {
					m.openAlertSelector()
					return m, nil
				}

				if s == "enter" && m.focus == len(m.inputs)-1 {
					if m.validateForm() {
						m.submitForm()
						m.refreshData()
					} else {
						m.updateFormContent()
					}
					return m, nil
				}

				if s == "up" || s == "shift+tab" {
					m.focus--
				} else {
					m.focus++
				}
				if m.focus > len(m.inputs)-1 {
					m.focus = 0
				}
				if m.focus < 0 {
					m.focus = len(m.inputs) - 1
				}

				for i := 0; i < len(m.inputs); i++ {
					if i == m.focus {
						cmds = append(cmds, m.inputs[i].Focus())
					} else {
						m.inputs[i].Blur()
					}
				}
				
				m.formViewport.SetYOffset(m.focus * 4)
				
				m.updateFormContent()
				return m, tea.Batch(cmds...)

			default:
				if m.state == stateFormSite && m.focus == 2 {
					return m, nil
				}
				m.updateFormContent()
			}
		}
	}

	if m.state == stateFormSite || m.state == stateFormAlert {
		for i := range m.inputs {
			m.inputs[i], cmd = m.inputs[i].Update(msg)
			cmds = append(cmds, cmd)
		}
		m.updateFormContent()
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) refreshData() {
	monitor.Mutex.RLock()
	var sites []models.Site
	for _, s := range monitor.LiveState {
		sites = append(sites, s)
	}
	monitor.Mutex.RUnlock()
	sort.Slice(sites, func(i, j int) bool { return sites[i].ID < sites[j].ID })
	m.sites = sites
	m.alerts = db.GetAllAlerts()

	logs := monitor.GetLogs()
	logContent := strings.Join(logs, "\n")
	m.logViewport.SetContent(logContent)
}

func (m *Model) initFormSite() {
	m.inputs = make([]textinput.Model, 5)
	m.inputs[0] = ti("https://example.com", 30)
	m.inputs[0].Focus()
	m.inputs[1] = ti("60", 10)
	m.inputs[2] = ti("", 20)
	m.inputs[3] = ti("n", 5)
	m.inputs[4] = ti("7", 5)
	m.focus = 0
	m.errorMsg = ""
}

func (m *Model) initFormAlert() {
	m.inputs = make([]textinput.Model, 3)
	m.inputs[0] = ti("Name", 20)
	m.inputs[0].Focus()
	m.inputs[1] = ti("discord", 20)
	m.inputs[1].SetValue("discord")
	m.inputs[2] = ti("Webhook URL", 50)
	m.focus = 0
	m.errorMsg = ""
}

func ti(ph string, width int) textinput.Model {
	t := textinput.New()
	t.Placeholder = ph
	t.Width = width
	return t
}

func (m *Model) openAlertSelector() {
	m.state = stateSelectAlert
	alerts := db.GetAllAlerts()
	var items []list.Item
	items = append(items, alertItem{id: -1, name: "+ Create New Alert", aType: "System"})
	for _, a := range alerts {
		items = append(items, alertItem{id: a.ID, name: a.Name, aType: a.Type})
	}
	m.alertList.SetItems(items)
	m.alertList.SetSize(m.formViewport.Width, m.formViewport.Height)
}

func (m *Model) updateFormContent() {
	var content string
	if m.errorMsg != "" {
		content += dangerStyle.Render("Error: "+m.errorMsg) + "\n\n"
	}

	if m.state == stateFormSite {
		if len(m.inputs) < 5 {
			return
		}
		title := "Add Monitor"
		if m.editID > 0 {
			title = fmt.Sprintf("Edit Monitor #%d", m.editID)
		}
		content += titleStyle.Render(title) + "\n\n"
		content += "URL:\n" + m.inputs[0].View() + "\n\n"
		content += "Interval (sec):\n" + m.inputs[1].View() + "\n\n"

		lbl := "Alert ID:"
		val := m.inputs[2].Value()
		if val == "" || val == "0" {
			val = "[Enter to Select]"
		} else {
			val = fmt.Sprintf("(ID: %s) [Enter to Change]", val)
		}
		if m.focus == 2 {
			lbl = specialStyle.Render(lbl)
			val = specialStyle.Render(val)
		}
		content += lbl + "\n" + val + "\n\n"
		content += "Check SSL? (y/n):\n" + m.inputs[3].View() + "\n\n"
		content += "SSL Warning Threshold (days):\n" + m.inputs[4].View() + "\n\n"

	} else if m.state == stateFormAlert {
		if len(m.inputs) < 3 {
			return
		}
		title := "Add Alert"
		if m.editID > 0 {
			title = fmt.Sprintf("Edit Alert #%d", m.editID)
		}
		content += titleStyle.Render(title) + "\n\n"
		content += "Name:\n" + m.inputs[0].View() + "\n\n"
		content += "Type:\n" + m.inputs[1].View() + "\n\n"
		content += "Webhook:\n" + m.inputs[2].View() + "\n\n"
	}
	m.formViewport.SetContent(lipgloss.NewStyle().Padding(1, 2).Render(content))
}

func (m *Model) validateForm() bool {
	if m.state == stateFormSite {
		if m.inputs[0].Value() == "" {
			m.errorMsg = "URL is required"
			return false
		}
	} else if m.state == stateFormAlert {
		if m.inputs[0].Value() == "" || m.inputs[2].Value() == "" {
			m.errorMsg = "Name and Webhook are required"
			return false
		}
	}
	return true
}

func (m *Model) submitForm() {
	if m.state == stateFormSite {
		url := m.inputs[0].Value()
		interval, _ := strconv.Atoi(m.inputs[1].Value())
		alertID, _ := strconv.Atoi(m.inputs[2].Value())
		checkSSL := false
		if strings.ToLower(m.inputs[3].Value()) == "y" {
			checkSSL = true
		}
		threshold, _ := strconv.Atoi(m.inputs[4].Value())
		if interval < 1 {
			interval = 60
		}
		if threshold < 1 {
			threshold = 7
		}

		if m.editID > 0 {
			db.UpdateSite(m.editID, url, interval, alertID, checkSSL, threshold)
			monitor.UpdateSiteConfig(m.editID, url, interval, alertID, checkSSL, threshold)
		} else {
			db.AddSite(url, interval, alertID, checkSSL, threshold)
		}
		m.state = stateDashboard

	} else if m.state == stateFormAlert {
		name := m.inputs[0].Value()
		atype := m.inputs[1].Value()
		url := m.inputs[2].Value()

		if m.editID > 0 {
			db.UpdateAlert(m.editID, name, atype, url)
		} else {
			db.AddAlert(name, atype, url)
		}

		if m.creatingAlertFromSite {
			m.creatingAlertFromSite = false
			alerts := db.GetAllAlerts()
			if len(alerts) > 0 {
				lastAlert := alerts[len(alerts)-1]
				m.state = stateFormSite
				m.initFormSite()
				m.inputs[2].SetValue(strconv.Itoa(lastAlert.ID))
				m.focus = 2
				m.updateFormContent()
			}
		} else {
			m.state = stateDashboard
		}
	}
}

func (m Model) View() string {
	switch m.state {
	case stateSelectAlert:
		f := subtleStyle.Render("\n[Enter] Select  [Esc] Cancel")
		return lipgloss.NewStyle().Padding(1, 2).Render(m.alertList.View()) + "\n" + f
	case stateFormSite, stateFormAlert:
		f := subtleStyle.Render("\n[Enter] Save  [PgUp/PgDn] Scroll  [Esc] Cancel")
		return m.formViewport.View() + "\n" + f
	default:
		return m.viewDashboard()
	}
}

func (m Model) viewDashboard() string {
	tabs := []string{"Sites", "Alerts", "Logs"}
	var renderedTabs []string
	for i, t := range tabs {
		if i == m.currentTab {
			renderedTabs = append(renderedTabs, activeTab.Render(t))
		} else {
			renderedTabs = append(renderedTabs, inactiveTab.Render(t))
		}
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
	content := ""

	if m.currentTab == 0 {
		headerStr := lipgloss.JoinHorizontal(lipgloss.Left,
			colID.Render("ID"), colURL.Render("URL"), colStatus.Render("STATUS"), colSSL.Render("SSL CERT"), colCode.Render("CODE"), "LATENCY")
		content += "\n" + headerStr + "\n"
		content += subtleStyle.Render(strings.Repeat("-", 80)) + "\n"

		end := m.tableOffset + m.maxTableRows
		if end > len(m.sites) {
			end = len(m.sites)
		}

		if len(m.sites) == 0 {
			content += "\n  No sites configured."
		} else {
			for i := m.tableOffset; i < end; i++ {
				site := m.sites[i]
				cursor := " "
				if m.cursor == i {
					cursor = ">"
				}

				statusStyle := specialStyle
				if site.Status == "DOWN" || site.Status == "SSL EXP" {
					statusStyle = dangerStyle
				} else if site.Status == "PENDING" {
					statusStyle = subtleStyle
				}

				sslStr := "-"
				if site.CheckSSL && site.HasSSL {
					days := int(time.Until(site.CertExpiry).Hours() / 24)
					s := fmt.Sprintf("%d days", days)
					if days <= 0 {
						sslStr = dangerStyle.Render("EXPIRED")
					} else if days <= site.ExpiryThreshold {
						sslStr = warnStyle.Render(s)
					} else {
						sslStr = specialStyle.Render(s)
					}
				}

				row := lipgloss.JoinHorizontal(lipgloss.Left,
					colID.Render(strconv.Itoa(site.ID)),
					colURL.Render(limitStr(site.URL, 28)),
					colStatus.Render(statusStyle.Render(site.Status)),
					colSSL.Render(sslStr),
					colCode.Render(strconv.Itoa(site.StatusCode)),
					fmt.Sprintf("%v", site.Latency.Round(time.Millisecond)),
				)

				if m.cursor == i {
					row = lipgloss.NewStyle().Bold(true).Render(cursor + row)
				} else {
					row = " " + row
				}
				content += row + "\n"
			}
		}
	} else if m.currentTab == 1 {
		content += fmt.Sprintf("\n%-3s %-15s %-10s %s\n", "ID", "NAME", "TYPE", "WEBHOOK")
		content += subtleStyle.Render("----------------------------------------------------------------") + "\n"

		end := m.tableOffset + m.maxTableRows
		if end > len(m.alerts) {
			end = len(m.alerts)
		}

		for i := m.tableOffset; i < end; i++ {
			alert := m.alerts[i]
			cursor := " "
			if m.cursor == i {
				cursor = ">"
			}
			row := fmt.Sprintf("%s %-3d %-15s %-10s %s",
				cursor, alert.ID, limitStr(alert.Name, 15), alert.Type, limitStr(alert.WebhookURL, 30))
			if m.cursor == i {
				row = lipgloss.NewStyle().Bold(true).Render(row)
			}
			content += row + "\n"
		}
	} else if m.currentTab == 2 {
		content += "\n" + m.logViewport.View()
	}

	footer := subtleStyle.Render("\n[n] New  [e/Enter] Edit  [d] Delete  [Tab] Switch View  [q] Quit")
	return lipgloss.NewStyle().Padding(1, 2).Render(header + "\n" + content + "\n" + footer)
}

func limitStr(text string, max int) string {
	if len(text) > max {
		return text[:max-3] + "..."
	}
	return text
}