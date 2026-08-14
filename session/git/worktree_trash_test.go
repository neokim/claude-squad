package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTrashTestRepo sets up a repo with one commit and a live worktree on a
// feature branch, with HOME pointed at a temp dir so the trash directory is
// isolated. It returns the worktree and the temp home.
func newTrashTestRepo(t *testing.T) (*GitWorktree, string) {
	t.Helper()

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	repoPath := filepath.Join(t.TempDir(), "repo")
	mustRunGit(t, "", "init", repoPath)
	mustRunGit(t, repoPath, "config", "user.name", "Test User")
	mustRunGit(t, repoPath, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRunGit(t, repoPath, "add", "README.md")
	mustRunGit(t, repoPath, "commit", "-m", "initial")

	worktreePath := filepath.Join(tempHome, ".claude-squad", "worktrees", "repo", "cs", "session")
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		t.Fatalf("mkdir worktrees: %v", err)
	}
	mustRunGit(t, repoPath, "worktree", "add", "-b", "feature/test", worktreePath)

	return &GitWorktree{
		repoPath:     repoPath,
		worktreePath: worktreePath,
		branchName:   "feature/test",
	}, tempHome
}

func TestMoveToTrash_DetachesWorktreeAndPreservesContentsAndBranch(t *testing.T) {
	g, tempHome := newTrashTestRepo(t)

	// A file that only exists in the worktree, so we can tell the contents were
	// moved rather than deleted.
	marker := filepath.Join(g.worktreePath, "marker.txt")
	if err := os.WriteFile(marker, []byte("payload\n"), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	trashPath, err := g.MoveToTrash()
	if err != nil {
		t.Fatalf("MoveToTrash() error = %v", err)
	}

	if trashPath == "" {
		t.Fatal("MoveToTrash() returned an empty trash path")
	}
	if want := filepath.Join(tempHome, ".claude-squad", "trash"); filepath.Dir(trashPath) != want {
		t.Fatalf("trash path %q is not under %q", trashPath, want)
	}

	// The worktree directory is gone from its original location...
	if _, err := os.Stat(g.worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree still at original path, err = %v", err)
	}
	// ...but its contents are intact in the trash, i.e. nothing was deleted on
	// the critical path.
	if _, err := os.Stat(filepath.Join(trashPath, "marker.txt")); err != nil {
		t.Fatalf("marker not found in trash: %v", err)
	}

	// git no longer knows about the worktree.
	if list := mustRunGit(t, g.repoPath, "worktree", "list"); strings.Contains(list, g.worktreePath) {
		t.Fatalf("worktree still registered with git:\n%s", list)
	}
	// The branch survives, which is the whole point of pausing.
	if refs := mustRunGit(t, g.repoPath, "branch", "--list", "feature/test"); !strings.Contains(refs, "feature/test") {
		t.Fatalf("branch feature/test was not preserved:\n%s", refs)
	}
}

func TestMoveToTrash_AllowsWorktreeToBeRecreated(t *testing.T) {
	g, _ := newTrashTestRepo(t)

	trashPath, err := g.MoveToTrash()
	if err != nil {
		t.Fatalf("MoveToTrash() error = %v", err)
	}
	DeleteTrashPath(trashPath)

	// This is what Resume does.
	g.isExistingBranch = true
	if err := g.Setup(); err != nil {
		t.Fatalf("Setup() after MoveToTrash() error = %v", err)
	}
	if valid, err := g.IsValidWorktree(); err != nil {
		t.Fatalf("IsValidWorktree() error = %v", err)
	} else if !valid {
		t.Fatal("expected the worktree to be recreated")
	}
}

func TestDeleteTrashPath_RemovesContents(t *testing.T) {
	g, _ := newTrashTestRepo(t)

	trashPath, err := g.MoveToTrash()
	if err != nil {
		t.Fatalf("MoveToTrash() error = %v", err)
	}

	DeleteTrashPath(trashPath)

	if _, err := os.Stat(trashPath); !os.IsNotExist(err) {
		t.Fatalf("trash path still exists after DeleteTrashPath, err = %v", err)
	}
}

func TestSweepTrash_RemovesLeftovers(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	trashDir := filepath.Join(tempHome, ".claude-squad", "trash")
	leftover := filepath.Join(trashDir, "session-123", "nested")
	if err := os.MkdirAll(leftover, 0755); err != nil {
		t.Fatalf("mkdir leftover: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write leftover file: %v", err)
	}

	SweepTrash()

	entries, err := os.ReadDir(trashDir)
	if err != nil {
		t.Fatalf("read trash dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("trash dir still has %d entries after SweepTrash", len(entries))
	}
}

func TestSweepTrash_NoTrashDirIsNotAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	SweepTrash() // must not panic
}
