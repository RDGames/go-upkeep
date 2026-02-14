package tui

import (
	"fmt"
	"go-upkeep/internal/models"
	"go-upkeep/internal/monitor"
	"go-upkeep/internal/store" 
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

	colID      = lipgloss.NewStyle().Width(4)
	colName    = lipgloss.NewStyle().Width(15)
	colURL     = lipgloss.NewStyle().Width(25)
	colStatus  = lipgloss.NewStyle().Width(8)
	colSSL     = lipgloss.NewStyle().Width(10)
	colType    = lipgloss.NewStyle().Width(6)
	colRetries = lipgloss.NewStyle().Width(6)
)

type alertItem struct {
	id          int
	name, aType string
}
func (i alertItem) Title() string { return i.name }
func (i alertItem) Description() string {
	if i.id == -1 { return "Set up a new notification channel" }
	return fmt.Sprintf("ID: %d | Type: %s", i.id, i.aType)
}
func (i alertItem) FilterValue() string { return i.name }

type sessionState int
const (
	stateDashboard sessionState = iota
	stateLogs
	stateUsers
	stateFormSite
	stateFormAlert
	stateFormUser
	stateSelectAlert
)

type Model struct {
	state      sessionState
	currentTab int
	cursor       int
	tableOffset  int
	maxTableRows int
	editID   int
	editToken string 

	siteInputs []textinput.Model
	alertInputs []textinput.Model
	userInputs []textinput.Model
	
	focus    int
	errorMsg string
	currentAlertType string

	logViewport  viewport.Model
	formViewport viewport.Model
	alertList    list.Model
	creatingAlertFromSite bool
	isAdmin bool 

	sites  []models.Site
	alerts []models.AlertConfig
	users  []models.User
}

func InitialModel(isAdmin bool) Model {
	vpLogs := viewport.New(100, 20)
	vpLogs.SetContent("Waiting for logs...")
	vpForm := viewport.New(100, 20)
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select Alert Config"
	l.SetShowHelp(false)
	return Model{state: stateDashboard, logViewport: vpLogs, formViewport: vpForm, alertList: l, maxTableRows: 5, currentAlertType: "discord", isAdmin: isAdmin}
}

func (m Model) Init() tea.Cmd {
	// UPDATED: Return ClearScreen to help with artifacting on startup
	return tea.Batch(tea.ClearScreen, tea.Tick(time.Second, func(t time.Time) tea.Msg { return t }))
}

func (m *Model) nextFocus(dir int) int {
	next := m.focus + dir
	if next > 7 { next = 0 }
	if next < 0 { next = 7 }
	isPush := m.siteInputs[1].Value() == "push"
	
	if isPush {
		if next == 2 { if dir > 0 { next = 3 } else { next = 1 } }
		if next == 5 || next == 6 { if dir > 0 { next = 7 } else { next = 4 } }
	}
	return next
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.maxTableRows = msg.Height - 10
		if m.maxTableRows < 1 { m.maxTableRows = 1 }
		m.logViewport.Width = msg.Width; m.logViewport.Height = msg.Height - 6
		m.formViewport.Width = msg.Width; m.formViewport.Height = msg.Height - 6
		m.alertList.SetSize(msg.Width, msg.Height-4)
		// Force a clear on resize (helps with attach sometimes)
		return m, tea.ClearScreen

	case time.Time:
		m.refreshData()
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return t })

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" { return m, tea.Quit }
		
		// UPDATED: Manual Redraw trigger for Docker Attach artifacts
		if msg.String() == "ctrl+l" {
			return m, tea.ClearScreen
		}

		if m.state == stateSelectAlert {
			switch msg.String() {
			case "esc": m.state = stateFormSite; m.updateFormContent(); return m, nil
			case "enter":
				selected := m.alertList.SelectedItem()
				if selected != nil {
					itm := selected.(alertItem)
					if itm.id == -1 {
						m.state = stateFormAlert
						m.initFormAlert()
						m.creatingAlertFromSite = true
						m.editID = 0
						// UPDATED: Explicitly reset viewport offset to fix cursor glitch
						m.formViewport.SetYOffset(0)
						m.formViewport.GotoTop()
						m.updateFormContent()
						return m, nil
					} else { m.siteInputs[4].SetValue(strconv.Itoa(itm.id)); m.state = stateFormSite; m.updateFormContent(); return m, nil }
				}
			}
			m.alertList, cmd = m.alertList.Update(msg); return m, cmd
		}

		switch m.state {
		case stateDashboard, stateLogs, stateUsers:
			switch msg.String() {
			case "q": return m, tea.Quit
			case "tab":
				m.currentTab++; 
				maxTabs := 2; if m.isAdmin { maxTabs = 3 }
				if m.currentTab > maxTabs { m.currentTab = 0 }
				m.cursor = 0; m.tableOffset = 0
				if m.currentTab == 2 { m.state = stateLogs } else if m.currentTab == 3 { m.state = stateUsers } else { m.state = stateDashboard }
			case "pgup", "pgdown":
				if m.state == stateLogs { m.logViewport, cmd = m.logViewport.Update(msg); return m, cmd }
			case "up", "k":
				if m.state == stateLogs { m.logViewport.LineUp(1) } else if m.cursor > 0 {
					m.cursor--; if m.cursor < m.tableOffset { m.tableOffset = m.cursor }
				}
			case "down", "j":
				if m.state == stateLogs { m.logViewport.LineDown(1) } else {
					max := len(m.sites) - 1
					if m.currentTab == 1 { max = len(m.alerts) - 1 }
					if m.currentTab == 3 { max = len(m.users) - 1 }
					if m.cursor < max {
						m.cursor++; if m.cursor >= m.tableOffset + m.maxTableRows { m.tableOffset++ }
					}
				}
			case "n":
				m.editID = 0; m.editToken = ""; m.errorMsg = ""; m.focus = 0
				if m.currentTab == 1 { m.state = stateFormAlert; m.initFormAlert()
				} else if m.currentTab == 3 && m.isAdmin { m.state = stateFormUser; m.initFormUser()
				} else if m.currentTab == 0 { m.state = stateFormSite; m.initFormSite() }
				
				// UPDATED: Reset Viewport to fix scroll glitch
				m.formViewport.SetYOffset(0)
				m.formViewport.GotoTop()
				
				m.updateFormContent(); return m, nil
			
			case "e", "enter":
				m.editID = 0; m.editToken = ""; m.errorMsg = ""; m.focus = 0
				if m.currentTab == 1 && len(m.alerts) > 0 {
					target := m.alerts[m.cursor]; m.editID = target.ID; m.state = stateFormAlert; m.initFormAlert()
					m.alertInputs[0].SetValue(target.Name); m.switchAlertType(target.Type)
					if target.Type == "email" {
						m.alertInputs[2].SetValue(target.Settings["host"]); m.alertInputs[3].SetValue(target.Settings["port"])
						m.alertInputs[4].SetValue(target.Settings["user"]); m.alertInputs[5].SetValue(target.Settings["pass"])
						m.alertInputs[6].SetValue(target.Settings["from"]); m.alertInputs[7].SetValue(target.Settings["to"])
					} else { m.alertInputs[2].SetValue(target.Settings["url"]) }
					
					m.formViewport.SetYOffset(0); m.formViewport.GotoTop(); m.updateFormContent(); return m, nil
				} else if m.currentTab == 0 && len(m.sites) > 0 {
					target := m.sites[m.cursor]; m.editID = target.ID; m.editToken = target.Token; m.state = stateFormSite; m.initFormSite()
					m.siteInputs[0].SetValue(target.Name)
					m.siteInputs[1].SetValue(target.Type)
					m.siteInputs[2].SetValue(target.URL)
					m.siteInputs[3].SetValue(strconv.Itoa(target.Interval))
					m.siteInputs[4].SetValue(strconv.Itoa(target.AlertID))
					sslVal := "n"; if target.CheckSSL { sslVal = "y" }; m.siteInputs[5].SetValue(sslVal)
					m.siteInputs[6].SetValue(strconv.Itoa(target.ExpiryThreshold)); m.siteInputs[7].SetValue(strconv.Itoa(target.MaxRetries))
					
					m.formViewport.SetYOffset(0); m.formViewport.GotoTop(); m.updateFormContent(); return m, nil
				}

			case "d", "backspace":
				if m.currentTab == 1 && len(m.alerts) > 0 {
					store.Get().DeleteAlert(m.alerts[m.cursor].ID); m.adjustCursor(len(m.alerts)-1)
				} else if m.currentTab == 0 && len(m.sites) > 0 {
					id := m.sites[m.cursor].ID; store.Get().DeleteSite(id); monitor.RemoveSite(id); m.adjustCursor(len(m.sites)-1)
				} else if m.currentTab == 3 && m.isAdmin && len(m.users) > 0 {
					store.Get().DeleteUser(m.users[m.cursor].ID); m.adjustCursor(len(m.users)-1)
				}
				m.refreshData()
			}

		case stateFormSite, stateFormAlert, stateFormUser:
			var currentInputs []textinput.Model
			if m.state == stateFormSite { currentInputs = m.siteInputs } else if m.state == stateFormAlert { currentInputs = m.alertInputs } else { currentInputs = m.userInputs }

			switch msg.String() {
			case "esc":
				if m.creatingAlertFromSite { m.creatingAlertFromSite = false; m.state = stateFormSite } else { m.state = stateDashboard; if m.currentTab == 3 { m.state = stateUsers } }
				m.updateFormContent(); return m, nil
			case "pgup", "pgdown":
				m.formViewport, cmd = m.formViewport.Update(msg); return m, cmd
			case "left", "right":
				if m.state == stateFormAlert && m.focus == 1 {
					types := []string{"discord", "slack", "webhook", "email"}
					currIdx := 0; for i, t := range types { if t == m.currentAlertType { currIdx = i } }
					if msg.String() == "right" { currIdx++ } else { currIdx-- }
					if currIdx >= len(types) { currIdx = 0 }; if currIdx < 0 { currIdx = len(types) - 1 }
					m.switchAlertType(types[currIdx]); m.updateFormContent(); return m, nil
				}
				if m.state == stateFormSite && m.focus == 1 {
					if m.siteInputs[1].Value() == "http" { m.siteInputs[1].SetValue("push") } else { m.siteInputs[1].SetValue("http") }
					m.updateFormContent(); return m, nil
				}
			case "tab", "shift+tab", "enter", "up", "down":
				s := msg.String()
				if m.state == stateFormSite && m.focus == 4 && s == "enter" { m.openAlertSelector(); return m, nil }
				
				if s == "enter" && m.focus == len(currentInputs)-1 {
					if m.validateForm() { m.submitForm(); m.refreshData() } else { m.updateFormContent() }
					return m, nil
				}
				
				dir := 1; if s == "up" || s == "shift+tab" { dir = -1 }
				
				if m.state == stateFormSite {
					m.focus = m.nextFocus(dir)
				} else {
					m.focus += dir
					if m.focus > len(currentInputs)-1 { m.focus = 0 }; if m.focus < 0 { m.focus = len(currentInputs) - 1 }
				}

				cmds = append(cmds, m.focusInputs()...)
				m.formViewport.SetYOffset(m.focus * 4); m.updateFormContent()
				return m, tea.Batch(cmds...)
			default:
				if m.state == stateFormSite { for i := range m.siteInputs { m.siteInputs[i], cmd = m.siteInputs[i].Update(msg); cmds = append(cmds, cmd) }
				} else if m.state == stateFormAlert { for i := range m.alertInputs { m.alertInputs[i], cmd = m.alertInputs[i].Update(msg); cmds = append(cmds, cmd) }
				} else if m.state == stateFormUser { for i := range m.userInputs { m.userInputs[i], cmd = m.userInputs[i].Update(msg); cmds = append(cmds, cmd) } }
				m.updateFormContent()
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) adjustCursor(newLen int) {
	if m.cursor >= newLen && m.cursor > 0 { m.cursor-- }
	if m.cursor < m.tableOffset { m.tableOffset = m.cursor; if m.tableOffset < 0 { m.tableOffset = 0 } }
}

func (m *Model) focusInputs() []tea.Cmd {
	var cmds []tea.Cmd
	var inputs []textinput.Model
	if m.state == stateFormSite { inputs = m.siteInputs } else if m.state == stateFormAlert { inputs = m.alertInputs } else { inputs = m.userInputs }
	
	for i := range inputs {
		if i == m.focus { cmds = append(cmds, inputs[i].Focus()) } else { inputs[i].Blur() }
	}
	return cmds
}

func (m *Model) refreshData() {
	monitor.Mutex.RLock(); var sites []models.Site; for _, s := range monitor.LiveState { sites = append(sites, s) }; monitor.Mutex.RUnlock()
	sort.Slice(sites, func(i, j int) bool { return sites[i].ID < sites[j].ID }); m.sites = sites
	if store.Get() != nil { 
		m.alerts = store.Get().GetAllAlerts() 
		if m.isAdmin { m.users = store.Get().GetAllUsers() }
	}
	m.logViewport.SetContent(strings.Join(monitor.GetLogs(), "\n"))
}

func (m *Model) initFormSite() {
	m.siteInputs = make([]textinput.Model, 8) 
	m.siteInputs[0] = ti("My Monitor", 30); m.siteInputs[0].Focus()
	m.siteInputs[1] = ti("http", 10); m.siteInputs[1].SetValue("http") 
	m.siteInputs[2] = ti("https://example.com", 30)
	m.siteInputs[3] = ti("60", 10)
	m.siteInputs[4] = ti("", 20)
	m.siteInputs[5] = ti("n", 5)
	m.siteInputs[6] = ti("7", 5)
	m.siteInputs[7] = ti("0", 5)
	m.focus = 0; m.errorMsg = ""
}

func (m *Model) initFormAlert() {
	m.currentAlertType = "discord"; m.switchAlertType("discord")
	m.focus = 0; m.errorMsg = ""
	if len(m.alertInputs) > 0 { m.alertInputs[0].SetValue("") }
}

func (m *Model) initFormUser() {
	m.userInputs = make([]textinput.Model, 2)
	m.userInputs[0] = ti("Username", 20); m.userInputs[0].Focus()
	m.userInputs[1] = ti("ssh-ed25519 AAA...", 50)
	m.focus = 0; m.errorMsg = ""
}

func (m *Model) switchAlertType(t string) {
	m.currentAlertType = t
	nameVal := ""; if len(m.alertInputs) > 0 { nameVal = m.alertInputs[0].Value() }
	
	if t == "email" {
		m.alertInputs = make([]textinput.Model, 8)
		m.alertInputs[0] = ti("Alert Name", 20); m.alertInputs[0].SetValue(nameVal)
		m.alertInputs[1] = ti(t, 20); m.alertInputs[1].SetValue(t)
		m.alertInputs[2] = ti("smtp.gmail.com", 30)
		m.alertInputs[3] = ti("587", 10)
		m.alertInputs[4] = ti("user@gmail.com", 30)
		m.alertInputs[5] = ti("password", 20); m.alertInputs[5].EchoMode = textinput.EchoPassword
		m.alertInputs[6] = ti("from@domain.com", 30)
		m.alertInputs[7] = ti("to@domain.com", 30)
	} else {
		m.alertInputs = make([]textinput.Model, 3)
		m.alertInputs[0] = ti("Alert Name", 20); m.alertInputs[0].SetValue(nameVal)
		m.alertInputs[1] = ti(t, 20); m.alertInputs[1].SetValue(t)
		m.alertInputs[2] = ti("Webhook URL", 50)
	}
	if m.focus >= len(m.alertInputs) { m.focus = len(m.alertInputs) - 1 }
	m.alertInputs[m.focus].Focus()
}

func ti(ph string, width int) textinput.Model {
	t := textinput.New(); t.Placeholder = ph; t.Width = width; return t
}

func (m *Model) openAlertSelector() {
	m.state = stateSelectAlert
	alerts := store.Get().GetAllAlerts()
	var items []list.Item
	items = append(items, alertItem{id: -1, name: "+ Create New Alert", aType: "System"})
	for _, a := range alerts { items = append(items, alertItem{id: a.ID, name: a.Name, aType: a.Type}) }
	m.alertList.SetItems(items); m.alertList.SetSize(m.formViewport.Width, m.formViewport.Height)
}

func (m *Model) updateFormContent() {
	var content string
	if m.errorMsg != "" { content += dangerStyle.Render("Error: "+m.errorMsg) + "\n\n" }

	if m.state == stateFormSite {
		title := "Add Monitor"; if m.editID > 0 { title = fmt.Sprintf("Edit Monitor #%d", m.editID) }
		content += titleStyle.Render(title) + "\n\n"
		
		content += "Name:\n" + m.siteInputs[0].View() + "\n\n"
		
		lbl := "Type (< Left / Right >):"
		val := strings.ToUpper(m.siteInputs[1].Value())
		if m.focus == 1 { lbl = specialStyle.Render(lbl); val = specialStyle.Render(val) }
		content += lbl + "\n" + val + "\n\n"
		
		isPush := m.siteInputs[1].Value() == "push"

		if !isPush {
			content += "URL:\n" + m.siteInputs[2].View() + "\n\n"
		} else {
			if m.editToken != "" {
				content += "Push URL (Secret!):\n" + subtleStyle.Render(fmt.Sprintf("GET /api/push?token=%s", m.editToken)) + "\n\n"
			} else {
				content += "Push URL:\n" + subtleStyle.Render("(Generated securely after saving)") + "\n\n"
			}
		}

		content += "Interval / Heartbeat (sec):\n" + m.siteInputs[3].View() + "\n\n"
		
		lbl = "Alert ID:"; val = m.siteInputs[4].Value()
		if val == "" || val == "0" { val = "[Enter to Select]" } else { val = fmt.Sprintf("(ID: %s) [Enter to Change]", val) }
		if m.focus == 4 { lbl = specialStyle.Render(lbl); val = specialStyle.Render(val) }
		content += lbl + "\n" + val + "\n\n"
		
		if !isPush {
			content += "Check SSL? (y/n):\n" + m.siteInputs[5].View() + "\n\n"
			content += "SSL Warning Threshold (days):\n" + m.siteInputs[6].View() + "\n\n"
		} else {
			content += subtleStyle.Render("SSL Checks disabled for Push monitors.") + "\n\n"
		}
		
		content += "Max Retries / Tolerance:\n" + m.siteInputs[7].View() + "\n\n"

	} else if m.state == stateFormAlert {
		title := "Add Alert"; if m.editID > 0 { title = fmt.Sprintf("Edit Alert #%d", m.editID) }
		content += titleStyle.Render(title) + "\n\n"
		content += "Name:\n" + m.alertInputs[0].View() + "\n\n"
		lbl := "Type ( < Left / Right > ):"; val := m.alertInputs[1].Value()
		if m.focus == 1 { lbl = specialStyle.Render(lbl); val = specialStyle.Render(val) }
		content += lbl + "\n" + val + "\n\n"
		
		if m.currentAlertType == "email" && len(m.alertInputs) >= 8 {
			content += "SMTP Host:\n" + m.alertInputs[2].View() + "\n\n" + "Port:\n" + m.alertInputs[3].View() + "\n\n" +
				"User:\n" + m.alertInputs[4].View() + "\n\n" + "Pass:\n" + m.alertInputs[5].View() + "\n\n" +
				"From Email:\n" + m.alertInputs[6].View() + "\n\n" + "To Email:\n" + m.alertInputs[7].View() + "\n\n"
		} else if len(m.alertInputs) >= 3 {
			content += "Webhook URL:\n" + m.alertInputs[2].View() + "\n\n"
		}
	} else if m.state == stateFormUser {
		title := "Add User (SSH Access)"
		content += titleStyle.Render(title) + "\n\n"
		content += "Username:\n" + m.userInputs[0].View() + "\n\n"
		content += "Public Key (ssh-ed25519...):\n" + m.userInputs[1].View() + "\n\n"
	}
	m.formViewport.SetContent(lipgloss.NewStyle().Padding(1, 2).Render(content))
}

func (m *Model) validateForm() bool {
	if m.state == stateFormSite { 
		if m.siteInputs[0].Value() == "" { m.errorMsg = "Name is required"; return false }
		if m.siteInputs[1].Value() == "http" && m.siteInputs[2].Value() == "" { m.errorMsg = "URL is required"; return false }
	}
	if m.state == stateFormAlert {
		if m.alertInputs[0].Value() == "" { m.errorMsg = "Name is required"; return false }
		if m.currentAlertType == "email" {
			if m.alertInputs[2].Value() == "" || m.alertInputs[7].Value() == "" { m.errorMsg = "Host/To required"; return false }
		} else { if m.alertInputs[2].Value() == "" { m.errorMsg = "URL required"; return false } }
	}
	if m.state == stateFormUser {
		if m.userInputs[0].Value() == "" || m.userInputs[1].Value() == "" { m.errorMsg = "Both fields required"; return false }
	}
	return true
}

func (m *Model) submitForm() {
	if m.state == stateFormSite {
		name := m.siteInputs[0].Value()
		sType := m.siteInputs[1].Value()
		url := m.siteInputs[2].Value()
		interval, _ := strconv.Atoi(m.siteInputs[3].Value())
		alertID, _ := strconv.Atoi(m.siteInputs[4].Value())
		checkSSL := false; if strings.ToLower(m.siteInputs[5].Value()) == "y" { checkSSL = true }
		threshold, _ := strconv.Atoi(m.siteInputs[6].Value())
		retries, _ := strconv.Atoi(m.siteInputs[7].Value())
		if interval < 1 { interval = 60 }
		if threshold < 1 { threshold = 7 }

		if m.editID > 0 {
			store.Get().UpdateSite(m.editID, name, url, sType, interval, alertID, checkSSL, threshold, retries)
			monitor.UpdateSiteConfig(m.editID, name, url, sType, interval, alertID, checkSSL, threshold, retries)
		} else { store.Get().AddSite(name, url, sType, interval, alertID, checkSSL, threshold, retries) }
		m.state = stateDashboard

	} else if m.state == stateFormAlert {
		name := m.alertInputs[0].Value()
		atype := m.alertInputs[1].Value()
		settings := make(map[string]string)
		if atype == "email" {
			settings["host"] = m.alertInputs[2].Value(); settings["port"] = m.alertInputs[3].Value()
			settings["user"] = m.alertInputs[4].Value(); settings["pass"] = m.alertInputs[5].Value()
			settings["from"] = m.alertInputs[6].Value(); settings["to"] = m.alertInputs[7].Value()
		} else { settings["url"] = m.alertInputs[2].Value() }

		if m.editID > 0 { store.Get().UpdateAlert(m.editID, name, atype, settings) } else { store.Get().AddAlert(name, atype, settings) }

		if m.creatingAlertFromSite {
			m.creatingAlertFromSite = false; alerts := store.Get().GetAllAlerts()
			if len(alerts) > 0 {
				last := alerts[len(alerts)-1]; m.state = stateFormSite
				m.siteInputs[4].SetValue(strconv.Itoa(last.ID)); m.focus = 4; m.updateFormContent()
			}
		} else { m.state = stateDashboard }
	} else if m.state == stateFormUser {
		store.Get().AddUser(m.userInputs[0].Value(), m.userInputs[1].Value(), "user")
		m.state = stateUsers
	}
}

// ... [View and other functions remain the same] ...
func (m Model) View() string {
	switch m.state {
	case stateSelectAlert:
		f := subtleStyle.Render("\n[Enter] Select  [Esc] Cancel")
		return lipgloss.NewStyle().Padding(1, 2).Render(m.alertList.View()) + "\n" + f
	case stateFormSite, stateFormAlert, stateFormUser:
		f := subtleStyle.Render("\n[Enter] Save  [PgUp/PgDn] Scroll  [Esc] Cancel")
		return m.formViewport.View() + "\n" + f
	default:
		return m.viewDashboard()
	}
}

func (m Model) viewDashboard() string {
	tabs := []string{"Sites", "Alerts", "Logs"}
	if m.isAdmin { tabs = append(tabs, "Users") }
	var renderedTabs []string
	for i, t := range tabs {
		if i == m.currentTab { renderedTabs = append(renderedTabs, activeTab.Render(t)) } else { renderedTabs = append(renderedTabs, inactiveTab.Render(t)) }
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
	content := ""

	if m.currentTab == 0 {
		headerStr := lipgloss.JoinHorizontal(lipgloss.Left, colID.Render("ID"), colName.Render("NAME"), colType.Render("TYPE"), colURL.Render("URL/DESC"), colStatus.Render("STATUS"), colSSL.Render("SSL CERT"), colRetries.Render("RETRY"))
		content += "\n" + headerStr + "\n" + subtleStyle.Render(strings.Repeat("-", 100)) + "\n"
		end := m.tableOffset + m.maxTableRows; if end > len(m.sites) { end = len(m.sites) }
		if len(m.sites) == 0 { content += "\n  No sites configured." } else {
			for i := m.tableOffset; i < end; i++ {
				site := m.sites[i]; cursor := " "; if m.cursor == i { cursor = ">" }
				statusStyle := specialStyle
				if site.Status == "DOWN" || site.Status == "SSL EXP" { statusStyle = dangerStyle } else if site.Status == "PENDING" { statusStyle = subtleStyle }
				sslStr := "-"
				if site.Type == "http" && site.CheckSSL && site.HasSSL {
					days := int(time.Until(site.CertExpiry).Hours() / 24); s := fmt.Sprintf("%d days", days)
					if days <= 0 { sslStr = dangerStyle.Render("EXPIRED") } else if days <= site.ExpiryThreshold { sslStr = warnStyle.Render(s) } else { sslStr = specialStyle.Render(s) }
				}
				retriesDone := site.FailureCount - 1; if retriesDone < 0 { retriesDone = 0 }
				dispCount := retriesDone; if dispCount > site.MaxRetries { dispCount = site.MaxRetries }
				retryStr := fmt.Sprintf("%d/%d", dispCount, site.MaxRetries)
				if site.Status == "UP" && site.FailureCount > 0 { retryStr = warnStyle.Render(retryStr) }
				if site.Status == "DOWN" { retryStr = dangerStyle.Render(retryStr) }
				urlDisplay := site.URL
				if site.Type == "push" { urlDisplay = "(Passive Monitor)" }
				row := lipgloss.JoinHorizontal(lipgloss.Left, colID.Render(strconv.Itoa(site.ID)), colName.Render(limitStr(site.Name, 14)), colType.Render(site.Type), colURL.Render(limitStr(urlDisplay, 24)), colStatus.Render(statusStyle.Render(site.Status)), colSSL.Render(sslStr), colRetries.Render(retryStr))
				if m.cursor == i { row = lipgloss.NewStyle().Bold(true).Render(cursor + row) } else { row = " " + row }
				content += row + "\n"
			}
		}
	} else if m.currentTab == 1 {
		content += fmt.Sprintf("\n%-3s %-15s %-10s %s\n", "ID", "NAME", "TYPE", "CONFIG")
		content += subtleStyle.Render("----------------------------------------------------------------") + "\n"
		end := m.tableOffset + m.maxTableRows; if end > len(m.alerts) { end = len(m.alerts) }
		for i := m.tableOffset; i < end; i++ {
			alert := m.alerts[i]; cursor := " "; if m.cursor == i { cursor = ">" }
			confStr := "settings..."
			if val, ok := alert.Settings["url"]; ok { confStr = limitStr(val, 30) }
			if alert.Type == "email" { confStr = fmt.Sprintf("SMTP: %s", alert.Settings["host"]) }
			row := fmt.Sprintf("%s %-3d %-15s %-10s %s", cursor, alert.ID, limitStr(alert.Name, 15), alert.Type, confStr)
			if m.cursor == i { row = lipgloss.NewStyle().Bold(true).Render(row) }
			content += row + "\n"
		}
	} else if m.currentTab == 2 {
		content += "\n" + m.logViewport.View()
	} else if m.currentTab == 3 && m.isAdmin {
		content += fmt.Sprintf("\n%-3s %-15s %-10s %s\n", "ID", "USER", "ROLE", "KEY")
		content += subtleStyle.Render("----------------------------------------------------------------") + "\n"
		end := m.tableOffset + m.maxTableRows; if end > len(m.users) { end = len(m.users) }
		for i := m.tableOffset; i < end; i++ {
			u := m.users[i]; cursor := " "; if m.cursor == i { cursor = ">" }
			row := fmt.Sprintf("%s %-3d %-15s %-10s %s", cursor, u.ID, limitStr(u.Username, 15), u.Role, limitStr(u.PublicKey, 40))
			if m.cursor == i { row = lipgloss.NewStyle().Bold(true).Render(row) }
			content += row + "\n"
		}
	}
	
	footer := subtleStyle.Render("\n[n] New  [e/Enter] Edit  [d] Delete  [Tab] Switch View  [Ctrl+L] Clear Screen  [q] Quit")
	if m.currentTab == 3 { footer = subtleStyle.Render("\n[n] Add User  [d] Revoke Access  [Tab] Switch View  [Ctrl+L] Clear Screen  [q] Quit") }
	return lipgloss.NewStyle().Padding(1, 2).Render(header + "\n" + content + "\n" + footer)
}

func limitStr(text string, max int) string {
	if len(text) > max { return text[:max-3] + "..." }
	return text
}