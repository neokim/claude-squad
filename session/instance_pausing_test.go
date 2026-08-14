package session

import "testing"

func TestPausedCoversPausing(t *testing.T) {
	tests := []struct {
		status  Status
		paused  bool
		pausing bool
	}{
		{Running, false, false},
		{Ready, false, false},
		{Loading, false, false},
		{Paused, true, false},
		{Pausing, true, true},
	}

	for _, tt := range tests {
		i := &Instance{Status: tt.status}
		if got := i.Paused(); got != tt.paused {
			t.Errorf("status %v: Paused() = %v, want %v", tt.status, got, tt.paused)
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
