package git

import (
	"claude-squad/log"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MaxBranchSearchResults is the maximum number of branches returned by SearchBranches.
const MaxBranchSearchResults = 50

// FetchBranches fetches and prunes remote-tracking branches (best-effort, won't fail if offline).
func FetchBranches(repoPath string) {
	cmd := exec.Command("git", "-C", repoPath, "fetch", "--prune")
	_ = cmd.Run()
}

// SearchBranches searches for branches whose name contains filter (case-insensitive),
// ordered by most recently updated first. Returns at most MaxBranchSearchResults.
// If filter is empty, returns all branches up to the limit.
func SearchBranches(repoPath, filter string) ([]string, error) {
	cmd := exec.Command("git", "-C", repoPath, "branch", "-a",
		"--sort=-committerdate",
		"--format=%(refname:short)")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %s (%w)", output, err)
	}

	seen := make(map[string]bool)
	var branches []string
	lower := strings.ToLower(filter)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "HEAD") {
			continue
		}
		name := strings.TrimPrefix(line, "origin/")
		if seen[name] {
			continue
		}
		seen[name] = true
		if filter != "" && !strings.Contains(strings.ToLower(name), lower) {
			continue
		}
		branches = append(branches, name)
		if len(branches) >= MaxBranchSearchResults {
			break
		}
	}
	return branches, nil
}

// runGitCommand executes a git command and returns any error
func (g *GitWorktree) runGitCommand(path string, args ...string) (string, error) {
	return g.runGitCommandEnv(path, nil, args...)
}

// runGitCommandEnv is like runGitCommand but appends extraEnv (e.g. GIT_INDEX_FILE)
// to the process environment.
func (g *GitWorktree) runGitCommandEnv(path string, extraEnv []string, args ...string) (string, error) {
	baseArgs := []string{"-C", path}
	cmd := exec.Command("git", append(baseArgs, args...)...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git command failed: %s (%w)", output, err)
	}

	return string(output), nil
}

// intentToAddEnv creates a throwaway git index seeded from the worktree's real
// index and stages untracked files into it with intent-to-add. This lets
// `git diff` include untracked files in its output without mutating the real
// .git index (which would prematurely stage deletions and leave new files as
// content-less intent-to-add entries, corrupting later `git add`/commit flows).
//
// It returns the env slice (GIT_INDEX_FILE=...) to pass to runGitCommandEnv and
// a cleanup func that removes the temporary index. cleanup is always safe to
// call, even on error.
func (g *GitWorktree) intentToAddEnv() (env []string, cleanup func(), err error) {
	cleanup = func() {}

	// Locate the real index file (linked worktrees keep their own index under
	// .git/worktrees/<id>/index).
	idxOut, err := g.runGitCommand(g.worktreePath, "rev-parse", "--git-path", "index")
	if err != nil {
		return nil, cleanup, err
	}
	realIdx := strings.TrimSpace(idxOut)
	if !filepath.IsAbs(realIdx) {
		realIdx = filepath.Join(g.worktreePath, realIdx)
	}

	tmp, err := os.CreateTemp("", "claude-squad-index-*")
	if err != nil {
		return nil, cleanup, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	cleanup = func() { _ = os.Remove(tmpPath) }

	// Seed the temp index from the real one so tracked files are recognized.
	// A brand-new worktree may not have an index yet; an empty temp index is
	// fine in that case.
	if data, rerr := os.ReadFile(realIdx); rerr == nil {
		if werr := os.WriteFile(tmpPath, data, 0o600); werr != nil {
			return nil, cleanup, werr
		}
	}

	env = []string{"GIT_INDEX_FILE=" + tmpPath}
	if _, aerr := g.runGitCommandEnv(g.worktreePath, env, "add", "-N", "."); aerr != nil {
		return nil, cleanup, aerr
	}
	return env, cleanup, nil
}

// PushChanges commits and pushes changes in the worktree to the remote branch
func (g *GitWorktree) PushChanges(commitMessage string, open bool) error {
	if err := checkGHCLI(); err != nil {
		return err
	}

	// Check if there are any changes to commit
	isDirty, err := g.IsDirty()
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if isDirty {
		// Stage all changes
		if _, err := g.runGitCommand(g.worktreePath, "add", "."); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to stage changes: %w", err)
		}

		// Create commit
		if _, err := g.runGitCommand(g.worktreePath, "commit", "-m", commitMessage, "--no-verify"); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to commit changes: %w", err)
		}
	}

	// First push the branch to remote to ensure it exists
	pushCmd := exec.Command("gh", "repo", "sync", "--source", "-b", g.branchName)
	pushCmd.Dir = g.worktreePath
	if err := pushCmd.Run(); err != nil {
		// If sync fails, try creating the branch on remote first
		gitPushCmd := exec.Command("git", "push", "-u", "origin", g.branchName)
		gitPushCmd.Dir = g.worktreePath
		if pushOutput, pushErr := gitPushCmd.CombinedOutput(); pushErr != nil {
			log.ErrorLog.Print(pushErr)
			return fmt.Errorf("failed to push branch: %s (%w)", pushOutput, pushErr)
		}
	}

	// Now sync with remote
	syncCmd := exec.Command("gh", "repo", "sync", "-b", g.branchName)
	syncCmd.Dir = g.worktreePath
	if output, err := syncCmd.CombinedOutput(); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to sync changes: %s (%w)", output, err)
	}

	// Open the branch in the browser
	if open {
		if err := g.OpenBranchURL(); err != nil {
			// Just log the error but don't fail the push operation
			log.ErrorLog.Printf("failed to open branch URL: %v", err)
		}
	}

	return nil
}

// CommitChanges commits changes locally without pushing to remote
func (g *GitWorktree) CommitChanges(commitMessage string) error {
	// Check if there are any changes to commit
	isDirty, err := g.IsDirty()
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if isDirty {
		// Stage all changes
		if _, err := g.runGitCommand(g.worktreePath, "add", "."); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to stage changes: %w", err)
		}

		// Create commit (local only)
		if _, err := g.runGitCommand(g.worktreePath, "commit", "-m", commitMessage, "--no-verify"); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to commit changes: %w", err)
		}
	}

	return nil
}

// IsDirty checks if the worktree has uncommitted changes
func (g *GitWorktree) IsDirty() (bool, error) {
	output, err := g.runGitCommand(g.worktreePath, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("failed to check worktree status: %w", err)
	}
	return len(output) > 0, nil
}

// IsValidWorktree reports whether the worktree path exists and contains a
// .git entry that still resolves to a live admin directory in the main repo.
// Returns (false, nil) when the worktree is orphaned (path missing, .git
// missing, or the gitdir target the .git pointer file references is gone).
func (g *GitWorktree) IsValidWorktree() (bool, error) {
	if _, err := os.Stat(g.worktreePath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat worktree path: %w", err)
	}
	gitEntry := filepath.Join(g.worktreePath, ".git")
	info, err := os.Stat(gitEntry)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat worktree .git: %w", err)
	}
	// A worktree's .git is a regular file containing "gitdir: <admin-dir>".
	// If the admin directory was removed (e.g. by `git worktree prune` while
	// the worktree directory was temporarily missing), all git operations on
	// this worktree fail with "not a git repository".
	if !info.IsDir() {
		gitdir, err := readWorktreeGitdir(gitEntry)
		if err != nil {
			return false, fmt.Errorf("failed to read worktree .git pointer: %w", err)
		}
		if gitdir == "" {
			return false, nil
		}
		if _, err := os.Stat(gitdir); err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to stat worktree gitdir: %w", err)
		}
	}
	return true, nil
}

// readWorktreeGitdir parses the "gitdir: <path>" line from a worktree .git
// pointer file. Returns an absolute path (resolving any relative reference
// against the pointer file's directory) or "" if the file lacks the line.
func readWorktreeGitdir(gitFile string) (string, error) {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
		if !ok {
			continue
		}
		gitdir := strings.TrimSpace(rest)
		if gitdir == "" {
			return "", nil
		}
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(filepath.Dir(gitFile), gitdir)
		}
		return gitdir, nil
	}
	return "", nil
}


// IsBranchCheckedOut checks if the instance branch is currently checked out
func (g *GitWorktree) IsBranchCheckedOut() (bool, error) {
	output, err := g.runGitCommand(g.repoPath, "branch", "--show-current")
	if err != nil {
		return false, fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)) == g.branchName, nil
}

// OpenBranchURL opens the branch URL in the default browser
func (g *GitWorktree) OpenBranchURL() error {
	// Check if GitHub CLI is available
	if err := checkGHCLI(); err != nil {
		return err
	}

	cmd := exec.Command("gh", "browse", "--branch", g.branchName)
	cmd.Dir = g.worktreePath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to open branch URL: %w", err)
	}
	return nil
}
