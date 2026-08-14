package app

import (
	"claude-squad/config"
	"claude-squad/keys"
	"claude-squad/session"
	"claude-squad/ui"
	"context"
	"encoding/json"
	"testing"
	"time"

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
		pauseResults: make(chan pauseDoneMsg, 4),
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

// Pausing blocks the session's own actions, but the user must still be able to
// look around and start other work.
func TestPausingLeavesNavigationAvailable(t *testing.T) {
	for _, name := range []keys.KeyName{
		keys.KeyUp, keys.KeyDown, keys.KeyTab, keys.KeyHelp, keys.KeyNew, keys.KeyQuit,
	} {
		assert.False(t, pausingBlockedKeys[name], "expected key %v to stay available while pausing", name)
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
	h, _ := newPauseTestHome(t, session.Paused)

	pressKey(h, "r")

	// Resume itself fails here (no real worktree), but it must have been
	// attempted rather than short-circuited by the pausing guard.
	assert.NotContains(t, h.errBox.String(), "still pausing")
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
	assert.Contains(t, h.errBox.String(), assert.AnError.Error())
}

// A metadata tick captured before the pause lands after it, on an instance that
// is now Pausing. It must not write a live status back: that would clear the
// spinner, re-enable every blocked key and let a second Pause start on top of
// the first.
func TestUpdate_MetadataTickDoesNotResurrectPausingInstance(t *testing.T) {
	h, instance := newPauseTestHome(t, session.Pausing)

	_, _ = h.Update(metadataUpdateDoneMsg{results: []instanceMetaResult{
		{instance: instance, updated: true},
	}})

	assert.Equal(t, session.Pausing, instance.Status)
}

// Pause reports failures that happened after the worktree was already gone, but
// those must not send the instance back to being active -- it has no worktree
// and Resume would refuse it.
func TestApplyPauseResult_RevertsOnlyWhenPauseDidNotHappen(t *testing.T) {
	h, instance := newPauseTestHome(t, session.Pausing)
	_ = h.applyPauseResult(pauseDoneMsg{instance: instance})
	assert.Equal(t, session.Paused, instance.Status)

	h2, instance2 := newPauseTestHome(t, session.Pausing)
	_ = h2.applyPauseResult(pauseDoneMsg{instance: instance2, err: assert.AnError})
	assert.Equal(t, session.Ready, instance2.Status)
}

// The pause runs for seconds, so the user has usually moved on by the time it
// lands. Regrouping the list must not drag the cursor back.
func TestApplyPauseResult_KeepsUserSelection(t *testing.T) {
	h, paused := newPauseTestHome(t, session.Pausing)

	other, err := session.NewInstance(session.InstanceOptions{
		Title: "other", Path: ".", Program: "echo",
	})
	require.NoError(t, err)
	h.list.AddInstance(other)()
	h.list.SelectInstance(other)

	_ = h.applyPauseResult(pauseDoneMsg{instance: paused})

	assert.Same(t, other, h.list.GetSelectedInstance())
}

// Quitting mid-pause would persist the session as paused while its worktree is
// untouched; the next Resume rebuilds the worktree and destroys the work the
// pause was in the middle of committing.
func TestHandleQuit_WaitsForInFlightPause(t *testing.T) {
	h, instance := newPauseTestHome(t, session.Pausing)

	started := make(chan struct{})
	h.pauseWG.Add(1)
	go func() {
		defer h.pauseWG.Done()
		close(started)
		time.Sleep(50 * time.Millisecond)
		h.pauseResults <- pauseDoneMsg{instance: instance}
	}()
	<-started

	_, _ = h.handleQuit()

	// The result was waited for and applied, not dropped.
	assert.Equal(t, session.Paused, instance.Status)
}
