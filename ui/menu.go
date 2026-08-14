package ui

import (
	"claude-squad/keys"
	"strings"

	"claude-squad/session"

	"github.com/charmbracelet/lipgloss"
)

var keyStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#655F5F",
	Dark:  "#7F7A7A",
})

var descStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#7A7474",
	Dark:  "#9C9494",
})

var sepStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#DDDADA",
	Dark:  "#3C3C3C",
})

var actionGroupStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("99"))

var separator = " • "
var verticalSeparator = " │ "

var menuStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("205"))

// MenuState represents different states the menu can be in
type MenuState int

const (
	StateDefault MenuState = iota
	StateEmpty
	StateNewInstance
	StatePrompt
)

type Menu struct {
	options       []keys.KeyName
	height, width int
	state         MenuState
	instance      *session.Instance
	activeTab     int

	// actionKeys lists keys that should be rendered with the action-group style.
	actionKeys map[keys.KeyName]bool
	// groupEnds is the option indices after which to render a vertical group separator.
	groupEnds map[int]bool

	// keyDown is the key which is pressed. The default is -1.
	keyDown keys.KeyName
}

var defaultMenuOptions = []keys.KeyName{keys.KeyNew, keys.KeyPrompt, keys.KeyHelp, keys.KeyQuit}
var newInstanceMenuOptions = []keys.KeyName{keys.KeySubmitName}
var promptMenuOptions = []keys.KeyName{keys.KeySubmitName}

func NewMenu() *Menu {
	return &Menu{
		options:   defaultMenuOptions,
		state:     StateEmpty,
		activeTab: 0,
		keyDown:   -1,
	}
}

func (m *Menu) Keydown(name keys.KeyName) {
	m.keyDown = name
}

func (m *Menu) ClearKeydown() {
	m.keyDown = -1
}

// SetState updates the menu state and options accordingly
func (m *Menu) SetState(state MenuState) {
	m.state = state
	m.updateOptions()
}

// SetInstance updates the current instance and refreshes menu options
func (m *Menu) SetInstance(instance *session.Instance) {
	m.instance = instance
	// Only change the state if we're not in a special state (NewInstance or Prompt)
	if m.state != StateNewInstance && m.state != StatePrompt {
		if m.instance != nil {
			m.state = StateDefault
		} else {
			m.state = StateEmpty
		}
	}
	m.updateOptions()
}

// SetActiveTab updates the currently active tab
func (m *Menu) SetActiveTab(tab int) {
	m.activeTab = tab
	m.updateOptions()
}

// updateOptions updates the menu options based on current state and instance
func (m *Menu) updateOptions() {
	m.actionKeys = nil
	m.groupEnds = nil
	switch m.state {
	case StateEmpty:
		m.setGroups([][]keys.KeyName{
			{keys.KeyNew, keys.KeyPrompt},
			{keys.KeyHelp, keys.KeyQuit},
		}, map[int]bool{0: true})
	case StateDefault:
		if m.instance != nil {
			// When there is an instance, show that instance's options
			m.addInstanceOptions()
		} else {
			// When there is no instance, show the empty state
			m.setGroups([][]keys.KeyName{
				{keys.KeyNew, keys.KeyPrompt},
				{keys.KeySearch, keys.KeyHelp, keys.KeyQuit},
			}, map[int]bool{0: true})
		}
	case StateNewInstance:
		m.options = newInstanceMenuOptions
	case StatePrompt:
		m.options = promptMenuOptions
	}
}

// setGroups assigns the menu's options as a flat list and records group boundaries +
// which keys should render with the action-group style.
func (m *Menu) setGroups(groups [][]keys.KeyName, actionGroupIdx map[int]bool) {
	m.options = nil
	m.actionKeys = make(map[keys.KeyName]bool)
	m.groupEnds = make(map[int]bool)

	cursor := 0
	for gi, g := range groups {
		for _, k := range g {
			m.options = append(m.options, k)
			if actionGroupIdx[gi] {
				m.actionKeys[k] = true
			}
		}
		cursor += len(g)
		// Record a separator after each group except the last.
		if gi != len(groups)-1 && len(g) > 0 {
			m.groupEnds[cursor-1] = true
		}
	}
}

func (m *Menu) addInstanceOptions() {
	// Loading and pausing instances only get minimal options -- nothing that
	// touches the worktree or the tmux session is safe mid-transition.
	if m.instance != nil && (m.instance.Status == session.Loading || m.instance.Pausing()) {
		m.options = []keys.KeyName{keys.KeyNew, keys.KeyHelp, keys.KeyQuit}
		return
	}

	// Instance management group
	mgmtGroup := []keys.KeyName{keys.KeyNew, keys.KeyKill}
	if m.instance.Status == session.Paused {
		mgmtGroup = append(mgmtGroup, keys.KeyRename)
	}

	// Action group
	actionGroup := []keys.KeyName{keys.KeyEnter}
	if m.instance.Status != session.Paused {
		// Attaching from a new OS terminal only makes sense while the tmux
		// session is alive — i.e. not paused.
		actionGroup = append(actionGroup, keys.KeyAttachExternal)
	}
	actionGroup = append(actionGroup, keys.KeySubmit)
	if m.instance.Status == session.Paused {
		actionGroup = append(actionGroup, keys.KeyResume)
	} else {
		actionGroup = append(actionGroup, keys.KeyCheckout, keys.KeyRestartInstance)
	}

	// Navigation group (when in diff tab)
	if m.activeTab == DiffTab || m.activeTab == TerminalTab {
		actionGroup = append(actionGroup, keys.KeyShiftUp)
	}

	// System group
	systemGroup := []keys.KeyName{keys.KeyTab, keys.KeySearch, keys.KeyHelp, keys.KeyQuit}

	m.setGroups(
		[][]keys.KeyName{mgmtGroup, actionGroup, systemGroup},
		map[int]bool{1: true},
	)
}

// SetSize sets the width of the window. The menu will be centered horizontally within this width.
func (m *Menu) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Menu) String() string {
	var s strings.Builder

	for i, k := range m.options {
		binding := keys.GlobalkeyBindings[k]

		var (
			localActionStyle = actionGroupStyle
			localKeyStyle    = keyStyle
			localDescStyle   = descStyle
		)
		if m.keyDown == k {
			localActionStyle = localActionStyle.Underline(true)
			localKeyStyle = localKeyStyle.Underline(true)
			localDescStyle = localDescStyle.Underline(true)
		}

		if m.actionKeys[k] {
			s.WriteString(localActionStyle.Render(binding.Help().Key))
			s.WriteString(" ")
			s.WriteString(localActionStyle.Render(binding.Help().Desc))
		} else {
			s.WriteString(localKeyStyle.Render(binding.Help().Key))
			s.WriteString(" ")
			s.WriteString(localDescStyle.Render(binding.Help().Desc))
		}

		// Add appropriate separator
		if i != len(m.options)-1 {
			if m.groupEnds[i] {
				s.WriteString(sepStyle.Render(verticalSeparator))
			} else {
				s.WriteString(sepStyle.Render(separator))
			}
		}
	}

	centeredMenuText := menuStyle.Render(s.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, centeredMenuText)
}
