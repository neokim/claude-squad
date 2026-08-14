package git

import (
	"claude-squad/config"
	"claude-squad/log"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Setup creates a new worktree for the session
func (g *GitWorktree) Setup() error {
	// Ensure worktrees directory exists early (can be done in parallel with branch check)
	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		return fmt.Errorf("failed to get worktree directory: %w", err)
	}

	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return err
	}

	// If this worktree uses a pre-existing branch, always set up from that branch
	// (it may exist locally or only on the remote).
	if g.isExistingBranch {
		return g.setupFromExistingBranch()
	}

	// Check if branch exists using git CLI (much faster than go-git PlainOpen)
	_, err = g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/heads/%s", g.branchName))
	if err == nil {
		return g.setupFromExistingBranch()
	}
	return g.setupNewWorktree()
}

// setupFromExistingBranch creates a worktree from an existing branch
func (g *GitWorktree) setupFromExistingBranch() error {
	// Directory already created in Setup(), skip duplicate creation

	// Clean up any existing worktree first
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath) // Ignore error if worktree doesn't exist
	// If the directory is still there (orphaned, not registered with git), drop it so `git worktree add` won't fail.
	_ = os.RemoveAll(g.worktreePath)

	// Check if the local branch exists
	_, localErr := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/heads/%s", g.branchName))
	if localErr != nil {
		// Local branch doesn't exist — check if remote tracking branch exists
		_, remoteErr := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/remotes/origin/%s", g.branchName))
		if remoteErr != nil {
			return fmt.Errorf("branch %s not found locally or on remote", g.branchName)
		}
		// Create a local tracking branch via worktree add -b
		if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, fmt.Sprintf("origin/%s", g.branchName)); err != nil {
			return fmt.Errorf("failed to create worktree from remote branch %s: %w", g.branchName, err)
		}
		return nil
	}

	// Create a new worktree from the existing local branch
	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", g.worktreePath, g.branchName); err != nil {
		return fmt.Errorf("failed to create worktree from branch %s: %w", g.branchName, err)
	}

	return nil
}

// setupNewWorktree creates a new worktree from HEAD
func (g *GitWorktree) setupNewWorktree() error {
	// Clean up any existing worktree first
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath) // Ignore error if worktree doesn't exist
	// If the directory is still there (orphaned, not registered with git), drop it so `git worktree add` won't fail.
	_ = os.RemoveAll(g.worktreePath)

	// Clean up any existing branch using git CLI (much faster than go-git PlainOpen)
	_, _ = g.runGitCommand(g.repoPath, "branch", "-D", g.branchName) // Ignore error if branch doesn't exist

	// Try to use 'develop' branch as the base; fall back to HEAD if it doesn't exist.
	baseRef := "develop"
	output, err := g.runGitCommand(g.repoPath, "rev-parse", baseRef)
	if err != nil {
		baseRef = "HEAD"
		output, err = g.runGitCommand(g.repoPath, "rev-parse", "HEAD")
		if err != nil {
			if strings.Contains(err.Error(), "fatal: ambiguous argument 'HEAD'") ||
				strings.Contains(err.Error(), "fatal: not a valid object name") ||
				strings.Contains(err.Error(), "fatal: HEAD: not a valid object name") {
				return fmt.Errorf("this appears to be a brand new repository: please create an initial commit before creating an instance")
			}
			return fmt.Errorf("failed to get HEAD commit hash: %w", err)
		}
	}
	baseCommit := strings.TrimSpace(string(output))
	g.baseCommitSHA = baseCommit

	// Create a new worktree from the base commit.
	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, baseCommit); err != nil {
		return fmt.Errorf("failed to create worktree from commit %s: %w", baseCommit, err)
	}

	return nil
}

// Cleanup removes the worktree and associated branch
func (g *GitWorktree) Cleanup() error {
	var errs []error

	// Detach the worktree first. This has to happen before the branch is
	// deleted: git refuses to delete a branch that a registered worktree is
	// still using. Like Pause, the contents are only renamed aside here and
	// deleted in the background, so killing a large session is not a stall.
	if _, err := os.Stat(g.worktreePath); err == nil {
		trashPath, err := g.MoveToTrash()
		if trashPath != "" {
			go DeleteTrashPath(trashPath)
		}
		if err != nil {
			errs = append(errs, err)
		}
	} else if !os.IsNotExist(err) {
		// Only append error if it's not a "not exists" error
		errs = append(errs, fmt.Errorf("failed to check worktree path: %w", err))
	} else if err := g.Prune(); err != nil {
		// The directory is already gone but git may still hold the registration.
		errs = append(errs, err)
	}

	// Delete the branch using git CLI, but skip if this is a pre-existing branch
	if !g.isExistingBranch {
		if _, err := g.runGitCommand(g.repoPath, "branch", "-D", g.branchName); err != nil {
			// Only log if it's not a "branch not found" error
			if !strings.Contains(err.Error(), "not found") {
				errs = append(errs, fmt.Errorf("failed to remove branch %s: %w", g.branchName, err))
			}
		}
	}

	if len(errs) > 0 {
		return g.combineErrors(errs)
	}

	return nil
}

// Remove removes the worktree but keeps the branch
func (g *GitWorktree) Remove() error {
	// Remove the worktree using git command
	if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	return nil
}

// MoveToTrash detaches the worktree from git without paying for the deletion
// of its contents. `git worktree remove -f` unlinks every file one by one,
// which on a large worktree (a Rust target/ or node_modules tree is easily
// 100k files) takes tens of seconds. Instead the directory is renamed into the
// trash dir — a constant-time rename, since trash/ sits on the same filesystem
// as worktrees/ — and git's now-dangling metadata is pruned.
//
// The returned path still holds the full contents; the caller is responsible
// for deleting it in the background (or leaving it for SweepTrash on the next
// startup). Falls back to a plain Remove if the rename is not possible.
func (g *GitWorktree) MoveToTrash() (string, error) {
	trashPath, err := MovePathToTrash(g.worktreePath)
	if err != nil {
		// Different filesystem, permissions, ... — fall back to the slow path
		// rather than leaving the worktree in place.
		log.WarningLog.Printf("could not move worktree %s to trash (%v); falling back to git worktree remove",
			g.worktreePath, err)
		if removeErr := g.Remove(); removeErr != nil {
			return "", removeErr
		}
		return "", g.Prune()
	}

	if err := g.Prune(); err != nil {
		// The worktree directory is already gone, so the session is effectively
		// paused; report the error but still hand back the trash path so the
		// caller can free the disk space.
		return trashPath, err
	}
	return trashPath, nil
}

// MovePathToTrash renames path into the trash directory and returns its new
// location. The rename is constant-time regardless of how much the directory
// holds, so callers can get out of the way of a slow delete and hand the
// returned path to a background RemoveAll.
func MovePathToTrash(path string) (string, error) {
	trashDir, err := config.GetTrashDir()
	if err != nil {
		return "", fmt.Errorf("failed to get trash directory: %w", err)
	}
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create trash directory: %w", err)
	}

	trashPath := filepath.Join(trashDir,
		fmt.Sprintf("%s-%d", filepath.Base(path), time.Now().UnixNano()))
	if err := os.Rename(path, trashPath); err != nil {
		return "", fmt.Errorf("failed to move %s to trash: %w", path, err)
	}
	return trashPath, nil
}

// DeleteTrashPath removes a path previously handed out by MovePathToTrash,
// logging how long the delete took. Intended to be run in its own goroutine.
func DeleteTrashPath(trashPath string) {
	start := time.Now()
	if err := os.RemoveAll(trashPath); err != nil {
		log.ErrorLog.Printf("failed to delete trashed worktree %s: %v", trashPath, err)
		return
	}
	log.InfoLog.Printf("deleted trashed worktree %s in %s", trashPath, time.Since(start))
}

// SweepTrash deletes everything left in the trash directory. Deletes are slow
// and are deliberately not awaited during a pause, so a crash or a quit can
// leave entries behind; this reclaims them. Safe to call concurrently with an
// in-flight delete — an entry that another goroutine already removed is
// skipped.
func SweepTrash() {
	trashDir, err := config.GetTrashDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(trashDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			log.WarningLog.Printf("could not sweep trash entry %s: %v", path, err)
		}
	}
}

// Prune removes all working tree administrative files and directories
func (g *GitWorktree) Prune() error {
	if _, err := g.runGitCommand(g.repoPath, "worktree", "prune"); err != nil {
		return fmt.Errorf("failed to prune worktrees: %w", err)
	}
	return nil
}

// CleanupWorktrees removes all worktrees and their associated branches
func CleanupWorktrees() error {
	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		return fmt.Errorf("failed to get worktree directory: %w", err)
	}

	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return fmt.Errorf("failed to read worktree directory: %w", err)
	}

	// Get a list of all branches associated with worktrees
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list worktrees: %w", err)
	}

	// Parse the output to extract branch names
	worktreeBranches := make(map[string]string)
	currentWorktree := ""
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			currentWorktree = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			branchPath := strings.TrimPrefix(line, "branch ")
			// Extract branch name from refs/heads/branch-name
			branchName := strings.TrimPrefix(branchPath, "refs/heads/")
			if currentWorktree != "" {
				worktreeBranches[currentWorktree] = branchName
			}
		}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			worktreePath := filepath.Join(worktreesDir, entry.Name())

			// Delete the branch associated with this worktree if found
			for path, branch := range worktreeBranches {
				if strings.Contains(path, entry.Name()) {
					// Delete the branch
					deleteCmd := exec.Command("git", "branch", "-D", branch)
					if err := deleteCmd.Run(); err != nil {
						// Log the error but continue with other worktrees
						log.ErrorLog.Printf("failed to delete branch %s: %v", branch, err)
					}
					break
				}
			}

			// Remove the worktree directory
			os.RemoveAll(worktreePath)
		}
	}

	// You have to prune the cleaned up worktrees.
	cmd = exec.Command("git", "worktree", "prune")
	_, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to prune worktrees: %w", err)
	}

	return nil
}
