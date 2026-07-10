package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiff_DoesNotMutateIndex verifies that computing a diff includes untracked
// files in the output while leaving the real git index untouched. Previously the
// diff used `git add -N .` against the real index, which pre-staged deletions and
// left new files as content-less intent-to-add entries, corrupting later
// `git add`/commit flows (a regression that recurred across sessions).
func TestDiff_DoesNotMutateIndex(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "repo")
	mustRunGit(t, "", "init", repoPath)
	mustRunGit(t, repoPath, "config", "user.name", "Test User")
	mustRunGit(t, repoPath, "config", "user.email", "test@example.com")

	writeFile(t, repoPath, "tracked.txt", "old\n")
	mustRunGit(t, repoPath, "add", "tracked.txt")
	mustRunGit(t, repoPath, "commit", "-m", "initial")
	base := strings.TrimSpace(mustRunGit(t, repoPath, "rev-parse", "HEAD"))

	// Mutate the working tree: delete a tracked file and create an untracked one.
	if err := os.Remove(filepath.Join(repoPath, "tracked.txt")); err != nil {
		t.Fatalf("remove tracked.txt: %v", err)
	}
	writeFile(t, repoPath, "new.txt", "new\n")

	g := &GitWorktree{
		repoPath:      repoPath,
		worktreePath:  repoPath,
		branchName:    "main",
		baseCommitSHA: base,
	}

	// Full diff must include the untracked file.
	stats := g.Diff()
	if stats.Error != nil {
		t.Fatalf("Diff() error = %v", stats.Error)
	}
	if !strings.Contains(stats.Content, "new.txt") {
		t.Fatalf("Diff() content missing untracked new.txt:\n%s", stats.Content)
	}
	if stats.Added != 1 || stats.Removed != 1 {
		t.Fatalf("Diff() added/removed = %d/%d, want 1/1", stats.Added, stats.Removed)
	}

	// Numstat variant must agree.
	num := g.DiffNumstat()
	if num.Error != nil {
		t.Fatalf("DiffNumstat() error = %v", num.Error)
	}
	if num.Added != 1 || num.Removed != 1 {
		t.Fatalf("DiffNumstat() added/removed = %d/%d, want 1/1", num.Added, num.Removed)
	}

	// The real index must be untouched: new.txt stays untracked (??) and the
	// deletion stays unstaged ( D), never staged (A / D in the left column).
	status := mustRunGit(t, repoPath, "status", "--porcelain")
	if !strings.Contains(status, "?? new.txt") {
		t.Fatalf("expected new.txt to remain untracked, status:\n%s", status)
	}
	if !strings.Contains(status, " D tracked.txt") {
		t.Fatalf("expected tracked.txt deletion to remain unstaged, status:\n%s", status)
	}

	// The definitive check: staging both files together must succeed. With a
	// polluted index the deletion would already be staged, making the pathspec
	// fail and atomically aborting the whole `git add`.
	mustRunGit(t, repoPath, "add", "new.txt", "tracked.txt")
	staged := mustRunGit(t, repoPath, "diff", "--cached", "--name-only")
	if !strings.Contains(staged, "new.txt") {
		t.Fatalf("new.txt not staged after git add, cached:\n%s", staged)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
