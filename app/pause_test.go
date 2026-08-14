package app

import (
	"claude-squad/config"
	"claude-squad/keys"
	"claude-squad/session"
	"claude-squad/ui"
	"context"
	"encoding/json"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memInstanceStorage is an in-memory config.InstanceStorage so tests don't touch
// the user's real state file.
type memInstanceStorage struct {
	data json.RawMessage
}

func (m *memInstanceStorage) SaveInstances(instancesJSON json.RawMessage) error {
	m.data = instancesJSON
	return nil
}

func (m *memInstanceStorage) GetInstances() json.RawMessage {
	if m.data == nil {
		return json.RawMessage("[]")
	}
	return m.data
}

func (m *memInstanceStorage) DeleteAllInstances() error {
	m.data = nil
	return nil
}

// newPauseTestHome builds a home wired with real UI components and in-memory
// storage, holding a single instance in the given status.
func newPauseTestHome(t *testing.T, status session.Status) (*home, *session.Instance) {
	t.Helper()

	storage, err := session.NewStorage(&memInstanceStorage{})
	require.NoError(t, err)

	h := &home{
		ctx:          context.Background(),
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
		errBox:       ui.NewErrBox(),
		storage:      storage,
		appConfig:    config.DefaultConfig(),
		state:        stateDefault,
	}
	h.list = ui.NewList(&h.spinner, false)
	h.errBox.SetSize(80, 1)

	instance, err := session.NewInstance(session.InstanceOptions{
		Title:   "pausing-session",
		Path:    ".",
		Program: "echo",
	})
	require.NoError(t, err)
	instance.Status = status
	h.list.AddInstance(instance)()
	h.list.SetSelectedInstance(0)

	return h, instance
}

// pressKey delivers a keypress the way the update loop does: the first call only
// highlights the menu entry and re-sends the key, the second one acts on it.
func pressKey(h *home, key string) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	_, _ = h.handleKeyPress(msg)
	_, _ = h.handleKeyPress(msg)
}

func TestIsPausingBlocked(t *testing.T) {
	// Anything that touches the worktree or the tmux session must wait.
	for _, name := range []keys.KeyName{
		keys.KeyEnter, keys.KeyAttachExternal, keys.KeyResume, keys.KeyCheckout,
		keys.KeyKill, keys.KeyRename, keys.KeySubmit, keys.KeyRestartInstance,
	} {
		assert.True(t, isPausingBlocked(name), "expected key %v to be blocked while pausing", name)
	}

	// Navigation and read-only actions stay available.
	for _, name := range []keys.KeyName{
		keys.KeyUp, keys.KeyDown, keys.KeyTab, keys.KeyHelp, keys.KeyNew, keys.KeyQuit,
	} {
		assert.False(t, isPausingBlocked(name), "expected key %v to stay available while pausing", name)
	}
}

func TestHandleKeyPress_ResumeRejectedWhilePausing(t *testing.T) {
	h, instance := newPauseTestHome(t, session.Pausing)

	pressKey(h, "r")

	// The instance must not have been touched; only pauseDoneMsg may move it on.
	assert.Equal(t, session.Pausing, instance.Status)
	assert.Contains(t, h.errBox.String(), "still pausing")
}

func TestHandleKeyPress_ResumeAllowedOncePaused(t *testing.T) {
	h, instance := newPauseTestHome(t, session.Paused)

	pressKey(h, "r")

	// Resume itself fails here (no real worktree), but it must have been
	// attempted rather than short-circuited by the pausing guard.
	assert.NotContains(t, h.errBox.String(), "still pausing")
	assert.Equal(t, session.Paused, instance.Status)
}

func TestUpdate_PauseDoneSettlesToPaused(t *testing.T) {
	h, instance := newPauseTestHome(t, session.Pausing)

	_, _ = h.Update(pauseDoneMsg{instance: instance})

	assert.Equal(t, session.Paused, instance.Status)
}

// A failed pause leaves the worktree in place, so the instance has to go back to
// being active rather than being stranded in Pausing forever.
func TestUpdate_PauseDoneErrorRestoresActiveStatus(t *testing.T) {
	h, instance := newPauseTestHome(t, session.Pausing)

	_, _ = h.Update(pauseDoneMsg{instance: instance, err: assert.AnError})

	assert.Equal(t, session.Ready, instance.Status)
	assert.False(t, instance.Pausing())
	assert.Contains(t, h.errBox.String(), assert.AnError.Error())
}
