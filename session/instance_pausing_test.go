package session

import (
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"path/filepath"
	"testing"
)

func TestInactiveCoversPausing(t *testing.T) {
	tests := []struct {
		status   Status
		inactive bool
		pausing  bool
	}{
		{Running, false, false},
		{Ready, false, false},
		{Loading, false, false},
		{Paused, true, false},
		{Pausing, true, true},
	}

	for _, tt := range tests {
		i := &Instance{Status: tt.status}
		if got := i.Inactive(); got != tt.inactive {
			t.Errorf("status %v: Inactive() = %v, want %v", tt.status, got, tt.inactive)
		}
		if got := i.Pausing(); got != tt.pausing {
			t.Errorf("status %v: Pausing() = %v, want %v", tt.status, got, tt.pausing)
		}
	}
}

// A pause in flight is driven by a goroutine that does not survive a restart.
// Persisting Pausing would reload the session into a state nothing advances, so
// it must be written out as Paused — which is accurate, because Pausing is only
// entered once the session is on its way out.
func TestToInstanceData_PersistsPausingAsPaused(t *testing.T) {
	i := &Instance{Title: "test", Status: Pausing}

	if got := i.ToInstanceData().Status; got != Paused {
		t.Errorf("serialized status = %v, want %v", got, Paused)
	}
	if i.Status != Pausing {
		t.Errorf("ToInstanceData mutated live status to %v", i.Status)
	}
}

func TestToInstanceData_PreservesOtherStatuses(t *testing.T) {
	for _, status := range []Status{Running, Ready, Loading, Paused} {
		i := &Instance{Title: "test", Status: status}
		if got := i.ToInstanceData().Status; got != status {
			t.Errorf("serialized status = %v, want %v", got, status)
		}
	}
}

// Resume must reject an instance whose pause has not settled yet: the worktree
// teardown may still be in flight, so recreating it would race.
func TestResume_RejectedWhilePausing(t *testing.T) {
	i := &Instance{Title: "test", Status: Pausing, started: true}

	if err := i.Resume(); err == nil {
		t.Fatal("expected Resume() to fail while pausing")
	}
}

func TestRestartInstance_RejectedWhilePausing(t *testing.T) {
	i := &Instance{Title: "test", Status: Pausing, started: true}

	if err := i.RestartInstance(); err == nil {
		t.Fatal("expected RestartInstance() to fail while pausing")
	}
}

func TestAttachExternal_RejectedWhilePausing(t *testing.T) {
	i := &Instance{Title: "test", Status: Pausing, started: true}

	if err := i.AttachExternal(); err == nil {
		t.Fatal("expected AttachExternal() to fail while pausing")
	}
}

// Pause runs on a background goroutine while the update loop reads Status on
// every render. Writing Status there would be a data race and would also race
// the update loop's own transition, so Pause must leave it alone.
func TestPause_LeavesStatusToTheCaller(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// A worktree whose directory never existed is orphaned, which is the one
	// Pause path that reaches the end without any git or tmux state to operate on.
	worktreePath := filepath.Join(t.TempDir(), "gone")
	i := &Instance{
		Title:       "test",
		Status:      Pausing,
		started:     true,
		tmuxSession: tmux.NewTmuxSession("test", "echo"),
		gitWorktree: git.NewGitWorktreeFromStorage(
			t.TempDir(), worktreePath, "test", "feature/test", "", false),
	}

	if err := i.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	if i.Status != Pausing {
		t.Fatalf("Pause() changed status to %v, want it left at %v", i.Status, Pausing)
	}
}
