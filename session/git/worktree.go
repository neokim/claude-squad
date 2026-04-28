package git

import (
	"claude-squad/config"
	"claude-squad/log"
	"fmt"
	"path/filepath"
	"time"
)

func getWorktreeDirectory() (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, "worktrees"), nil
}

// GitWorktree manages git worktree operations for a session
type GitWorktree struct {
	// Path to the repository
	repoPath string
	// Path to the worktree
	worktreePath string
	// Name of the session
	sessionName string
	// Branch name for the worktree
	branchName string
	// Base commit hash for the worktree
	baseCommitSHA string
	// isExistingBranch is true if the branch existed before the session was created.
	// When true, the branch will not be deleted on cleanup.
	isExistingBranch bool
}

func NewGitWorktreeFromStorage(repoPath string, worktreePath string, sessionName string, branchName string, baseCommitSHA string, isExistingBranch bool) *GitWorktree {
	return &GitWorktree{
		repoPath:         repoPath,
		worktreePath:     worktreePath,
		sessionName:      sessionName,
		branchName:       branchName,
		baseCommitSHA:    baseCommitSHA,
		isExistingBranch: isExistingBranch,
	}
}

// resolveWorktreePaths resolves the repo root and generates a unique worktree path for the given branch name.
func resolveWorktreePaths(repoPath string, branchName string) (resolvedRepo string, worktreePath string, err error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		log.ErrorLog.Printf("git worktree path abs error, falling back to repoPath %s: %s", repoPath, err)
		absPath = repoPath
	}

	resolvedRepo, err = findGitRepoRoot(absPath)
	if err != nil {
		return "", "", err
	}

	worktreeDir, err := getWorktreeDirectory()
	if err != nil {
		return "", "", err
	}

	worktreePath = filepath.Join(worktreeDir, sanitizeBranchName(branchName))
	worktreePath = worktreePath + "_" + fmt.Sprintf("%x", time.Now().UnixNano())

	return resolvedRepo, worktreePath, nil
}

// NewGitWorktree creates a new GitWorktree instance
func NewGitWorktree(repoPath string, sessionName string) (tree *GitWorktree, branchname string, err error) {
	cfg := config.LoadConfig()
	branchName := fmt.Sprintf("%s%s", cfg.BranchPrefix, sessionName)
	// Sanitize the final branch name to handle invalid characters from any source
	// (e.g., backslashes from Windows domain usernames like DOMAIN\user)
	branchName = sanitizeBranchName(branchName)

	repoPath, worktreePath, err := resolveWorktreePaths(repoPath, branchName)
	if err != nil {
		return nil, "", err
	}

	return &GitWorktree{
		repoPath:     repoPath,
		sessionName:  sessionName,
		branchName:   branchName,
		worktreePath: worktreePath,
	}, branchName, nil
}

// NewGitWorktreeFromBranch creates a new GitWorktree that uses an existing branch.
// The branch will not be deleted on cleanup.
func NewGitWorktreeFromBranch(repoPath string, branchName string, sessionName string) (*GitWorktree, error) {
	repoPath, worktreePath, err := resolveWorktreePaths(repoPath, branchName)
	if err != nil {
		return nil, err
	}

	return &GitWorktree{
		repoPath:         repoPath,
		sessionName:      sessionName,
		branchName:       branchName,
		worktreePath:     worktreePath,
		isExistingBranch: true,
	}, nil
}

// IsExistingBranch returns whether this worktree uses a pre-existing branch
func (g *GitWorktree) IsExistingBranch() bool {
	return g.isExistingBranch
}

// GetWorktreePath returns the path to the worktree
func (g *GitWorktree) GetWorktreePath() string {
	return g.worktreePath
}

// GetBranchName returns the name of the branch associated with this worktree
func (g *GitWorktree) GetBranchName() string {
	return g.branchName
}

// GetRepoPath returns the path to the repository
func (g *GitWorktree) GetRepoPath() string {
	return g.repoPath
}

// GetRepoName returns the name of the repository (last part of the repoPath).
func (g *GitWorktree) GetRepoName() string {
	return filepath.Base(g.repoPath)
}

// GetBaseCommitSHA returns the base commit SHA for the worktree
func (g *GitWorktree) GetBaseCommitSHA() string {
	return g.baseCommitSHA
}

// PlanRename computes the new branch name and worktree path that would result from
// renaming a session to newSessionName. Computing these up front lets callers validate
// collisions across all related artifacts (branch / worktree dir / external history)
// before any side effects are applied.
func PlanRename(newSessionName string) (newBranchName, newWorktreePath string, err error) {
	cfg := config.LoadConfig()
	newBranchName = sanitizeBranchName(fmt.Sprintf("%s%s", cfg.BranchPrefix, newSessionName))
	if newBranchName == "" {
		return "", "", fmt.Errorf("new branch name is empty after sanitization")
	}

	worktreeDir, err := getWorktreeDirectory()
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve worktree directory: %w", err)
	}
	newWorktreePath = filepath.Join(worktreeDir, newBranchName) + "_" + fmt.Sprintf("%x", time.Now().UnixNano())
	return newBranchName, newWorktreePath, nil
}

// BranchExists returns true if a local branch with the given name already exists.
func (g *GitWorktree) BranchExists(branchName string) bool {
	_, err := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/heads/%s", branchName))
	return err == nil
}

// RenameBranch renames the underlying git branch and reassigns sessionName/worktreePath
// to the externally-provided values. The caller is expected to have validated that
// newBranchName does not already exist; we re-check defensively. The worktree
// directory must not be currently checked out (i.e., the instance is paused).
func (g *GitWorktree) RenameBranch(newSessionName, newBranchName, newWorktreePath string) error {
	if newBranchName == g.branchName {
		// Title differs but produces the same sanitized branch name; nothing to do at the git layer.
		g.sessionName = newSessionName
		g.worktreePath = newWorktreePath
		return nil
	}
	if newBranchName == "" {
		return fmt.Errorf("new branch name is empty")
	}
	if g.BranchExists(newBranchName) {
		return fmt.Errorf("branch %s already exists", newBranchName)
	}
	if _, err := g.runGitCommand(g.repoPath, "branch", "-m", g.branchName, newBranchName); err != nil {
		return fmt.Errorf("failed to rename branch %s -> %s: %w", g.branchName, newBranchName, err)
	}

	g.branchName = newBranchName
	g.sessionName = newSessionName
	g.worktreePath = newWorktreePath
	return nil
}

// RollbackBranchRename reverts a previous branch rename in the git repo and restores
// the in-memory worktreePath. Returns an error if the git rename fails; callers should
// log it as a degraded-state warning since a failed rollback leaves git/in-memory state
// out of sync.
func (g *GitWorktree) RollbackBranchRename(prevBranchName, prevWorktreePath string) error {
	if g.branchName == prevBranchName {
		return nil
	}
	if _, err := g.runGitCommand(g.repoPath, "branch", "-m", g.branchName, prevBranchName); err != nil {
		return fmt.Errorf("failed to roll back branch rename: %w", err)
	}
	g.branchName = prevBranchName
	g.worktreePath = prevWorktreePath
	return nil
}
