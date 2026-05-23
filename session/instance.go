package session

import (
	"bytes"
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"path/filepath"
	"runtime"

	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

type Status int

const (
	// Running is the status when the instance is running and claude is working.
	Running Status = iota
	// Ready is if the claude instance is ready to be interacted with (waiting for user input).
	Ready
	// Loading is if the instance is loading (if we are starting it up or something).
	Loading
	// Paused is if the instance is paused (worktree removed but branch preserved).
	Paused
)

// Instance is a running instance of claude code.
type Instance struct {
	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Branch is the branch of the instance.
	Branch string
	// Status is the status of the instance.
	Status Status
	// Program is the program to run in the instance.
	Program string
	// Height is the height of the instance.
	Height int
	// Width is the width of the instance.
	Width int
	// CreatedAt is the time the instance was created.
	CreatedAt time.Time
	// UpdatedAt is the time the instance was last updated.
	UpdatedAt time.Time
	// AutoYes is true if the instance should automatically press enter when prompted.
	AutoYes bool
	// Prompt is the initial prompt to pass to the instance on startup
	Prompt string

	// DiffStats stores the current git diff statistics
	diffStats *git.DiffStats

	// selectedBranch is the existing branch to start on (empty = new branch from HEAD)
	selectedBranch string

	// The below fields are initialized upon calling Start().

	started bool
	// tmuxSession is the tmux session for the instance.
	tmuxSession *tmux.TmuxSession
	// gitWorktree is the git worktree for the instance.
	gitWorktree *git.GitWorktree
}

// ToInstanceData converts an Instance to its serializable form
func (i *Instance) ToInstanceData() InstanceData {
	data := InstanceData{
		Title:     i.Title,
		Path:      i.Path,
		Branch:    i.Branch,
		Status:    i.Status,
		Height:    i.Height,
		Width:     i.Width,
		CreatedAt: i.CreatedAt,
		UpdatedAt: time.Now(),
		Program:   i.Program,
		AutoYes:   i.AutoYes,
	}

	// Only include worktree data if gitWorktree is initialized
	if i.gitWorktree != nil {
		data.Worktree = GitWorktreeData{
			RepoPath:         i.gitWorktree.GetRepoPath(),
			WorktreePath:     i.gitWorktree.GetWorktreePath(),
			SessionName:      i.Title,
			BranchName:       i.gitWorktree.GetBranchName(),
			BaseCommitSHA:    i.gitWorktree.GetBaseCommitSHA(),
			IsExistingBranch: i.gitWorktree.IsExistingBranch(),
		}
	}

	// Only include diff stats if they exist
	if i.diffStats != nil {
		data.DiffStats = DiffStatsData{
			Added:   i.diffStats.Added,
			Removed: i.diffStats.Removed,
			Content: i.diffStats.Content,
		}
	}

	return data
}

// FromInstanceData creates a new Instance from serialized data
func FromInstanceData(data InstanceData) (*Instance, error) {
	instance := &Instance{
		Title:     data.Title,
		Path:      data.Path,
		Branch:    data.Branch,
		Status:    data.Status,
		Height:    data.Height,
		Width:     data.Width,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
		Program:   data.Program,
		gitWorktree: git.NewGitWorktreeFromStorage(
			data.Worktree.RepoPath,
			data.Worktree.WorktreePath,
			data.Worktree.SessionName,
			data.Worktree.BranchName,
			data.Worktree.BaseCommitSHA,
			data.Worktree.IsExistingBranch,
		),
		diffStats: &git.DiffStats{
			Added:   data.DiffStats.Added,
			Removed: data.DiffStats.Removed,
			Content: data.DiffStats.Content,
		},
	}

	if instance.Paused() {
		instance.started = true
		instance.tmuxSession = tmux.NewTmuxSession(instance.Title, instance.Program)
	} else {
		if err := instance.Start(false); err != nil {
			return nil, err
		}
	}

	return instance, nil
}

// Options for creating a new instance
type InstanceOptions struct {
	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Program is the program to run in the instance (e.g. "claude", "aider --model ollama_chat/gemma3:1b")
	Program string
	// If AutoYes is true, then
	AutoYes bool
	// Branch is an existing branch name to start the session on (empty = new branch from HEAD)
	Branch string
}

func NewInstance(opts InstanceOptions) (*Instance, error) {
	t := time.Now()

	// Convert path to absolute
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	return &Instance{
		Title:          opts.Title,
		Status:         Ready,
		Path:           absPath,
		Program:        opts.Program,
		Height:         0,
		Width:          0,
		CreatedAt:      t,
		UpdatedAt:      t,
		AutoYes:        false,
		selectedBranch: opts.Branch,
	}, nil
}

func (i *Instance) RepoName() (string, error) {
	if !i.started {
		return "", fmt.Errorf("cannot get repo name for instance that has not been started")
	}
	return i.gitWorktree.GetRepoName(), nil
}

func (i *Instance) SetStatus(status Status) {
	i.Status = status
}

// SetSelectedBranch sets the branch to use when starting the instance.
func (i *Instance) SetSelectedBranch(branch string) {
	i.selectedBranch = branch
}

// firstTimeSetup is true if this is a new instance. Otherwise, it's one loaded from storage.
func (i *Instance) Start(firstTimeSetup bool) error {
	if i.Title == "" {
		return fmt.Errorf("instance title cannot be empty")
	}

	var tmuxSession *tmux.TmuxSession
	if i.tmuxSession != nil {
		// Use existing tmux session (useful for testing)
		tmuxSession = i.tmuxSession
	} else {
		// Create new tmux session
		tmuxSession = tmux.NewTmuxSession(i.Title, i.Program)
	}
	i.tmuxSession = tmuxSession

	if firstTimeSetup {
		if i.selectedBranch != "" {
			gitWorktree, err := git.NewGitWorktreeFromBranch(i.Path, i.selectedBranch, i.Title)
			if err != nil {
				return fmt.Errorf("failed to create git worktree from branch: %w", err)
			}
			i.gitWorktree = gitWorktree
			i.Branch = i.selectedBranch
		} else {
			gitWorktree, branchName, err := git.NewGitWorktree(i.Path, i.Title)
			if err != nil {
				return fmt.Errorf("failed to create git worktree: %w", err)
			}
			i.gitWorktree = gitWorktree
			i.Branch = branchName
		}
	}

	// Setup error handler to cleanup resources on any error
	var setupErr error
	defer func() {
		if setupErr != nil {
			if cleanupErr := i.Kill(); cleanupErr != nil {
				setupErr = fmt.Errorf("%v (cleanup error: %v)", setupErr, cleanupErr)
			}
		} else {
			i.started = true
		}
	}()

	if !firstTimeSetup {
		// Reuse existing session
		if err := tmuxSession.Restore(); err != nil {
			setupErr = fmt.Errorf("failed to restore existing session: %w", err)
			return setupErr
		}
	} else {
		// Setup git worktree first
		if err := i.gitWorktree.Setup(); err != nil {
			setupErr = fmt.Errorf("failed to setup git worktree: %w", err)
			return setupErr
		}

		// Create new session
		if err := i.tmuxSession.Start(i.gitWorktree.GetWorktreePath()); err != nil {
			// Cleanup git worktree if tmux session creation fails
			if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
			setupErr = fmt.Errorf("failed to start new session: %w", err)
			return setupErr
		}
	}

	i.SetStatus(Running)

	return nil
}

// Kill terminates the instance and cleans up all resources
func (i *Instance) Kill() error {
	if !i.started {
		// If instance was never started, just return success
		return nil
	}

	var errs []error

	// Always try to cleanup both resources, even if one fails
	// Clean up tmux session first since it's using the git worktree
	if i.tmuxSession != nil {
		if err := i.tmuxSession.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close tmux session: %w", err))
		}
	}

	// Then clean up git worktree
	if i.gitWorktree != nil {
		if err := i.gitWorktree.Cleanup(); err != nil {
			errs = append(errs, fmt.Errorf("failed to cleanup git worktree: %w", err))
		}
	}

	return i.combineErrors(errs)
}

// combineErrors combines multiple errors into a single error
func (i *Instance) combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple cleanup errors occurred:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return fmt.Errorf("%s", errMsg)
}

// IsWorktreeOrphan reports whether the instance's git worktree is missing the
// state git needs (worktree dir, .git pointer, or admin gitdir target). When
// true, normal Pause operations like dirty-check or `git worktree remove`
// will fail and the worktree dir must be cleaned up directly.
func (i *Instance) IsWorktreeOrphan() (bool, error) {
	if !i.started || i.gitWorktree == nil || i.Status == Paused {
		return false, nil
	}
	valid, err := i.gitWorktree.IsValidWorktree()
	if err != nil {
		return false, err
	}
	return !valid, nil
}

func (i *Instance) Preview() (string, error) {
	if !i.started || i.Status == Paused {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContent()
}

func (i *Instance) HasUpdated() (updated bool, hasPrompt bool) {
	if !i.started {
		return false, false
	}
	return i.tmuxSession.HasUpdated()
}

// CheckAndHandleTrustPrompt checks for and dismisses the trust prompt for supported programs.
func (i *Instance) CheckAndHandleTrustPrompt() bool {
	if !i.started || i.tmuxSession == nil {
		return false
	}
	program := i.Program
	if !strings.HasSuffix(program, tmux.ProgramClaude) &&
		!strings.HasSuffix(program, tmux.ProgramAider) &&
		!strings.HasSuffix(program, tmux.ProgramGemini) {
		return false
	}
	return i.tmuxSession.CheckAndHandleTrustPrompt()
}

// TapEnter sends an enter key press to the tmux session if AutoYes is enabled.
func (i *Instance) TapEnter() {
	if !i.started || !i.AutoYes {
		return
	}
	if err := i.tmuxSession.TapEnter(); err != nil {
		log.ErrorLog.Printf("error tapping enter: %v", err)
	}
}

func (i *Instance) Attach() (chan struct{}, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot attach instance that has not been started")
	}
	return i.tmuxSession.Attach()
}

// AttachExternal opens a new OS terminal window and attaches to this instance's
// tmux session as an additional client. The original claude-squad TUI keeps its
// own connection — this gives the user a separate window viewing the same
// session.
func (i *Instance) AttachExternal() error {
	if !i.started {
		return fmt.Errorf("cannot attach: instance has not been started yet")
	}
	if i.Status == Loading {
		return fmt.Errorf("cannot attach: instance is still loading")
	}
	if i.Status == Paused {
		return fmt.Errorf("cannot attach: instance is paused (resume it first with 'r')")
	}
	if !i.tmuxSession.DoesSessionExist() {
		return fmt.Errorf("cannot attach: tmux session is not alive")
	}

	sessionName := i.tmuxSession.GetSessionName()
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux binary not found: %w", err)
	}
	attachCmd := fmt.Sprintf("%s attach -t %s", shellQuote(tmuxBin), shellQuote(sessionName))

	if runtime.GOOS != "darwin" {
		return fmt.Errorf("opening external terminal is only supported on macOS")
	}

	termProgram := os.Getenv("TERM_PROGRAM")
	var script string
	switch termProgram {
	case "iTerm.app":
		script = fmt.Sprintf(
			`tell application "iTerm"
				activate
				create window with default profile command %s
			end tell`,
			appleScriptQuote(attachCmd),
		)
	default:
		// Apple_Terminal or unknown — default to Terminal.app, which is always
		// present on macOS.
		script = fmt.Sprintf(
			`tell application "Terminal"
				activate
				do script %s
			end tell`,
			appleScriptQuote(attachCmd),
		)
	}

	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to open external terminal: %w", err)
	}
	return nil
}

// shellQuote wraps s in single quotes for safe use in a /bin/sh command line.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// appleScriptQuote wraps s in double quotes for safe use as an AppleScript
// string literal.
func appleScriptQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func (i *Instance) SetPreviewSize(width, height int) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot set preview size for instance that has not been started or " +
			"is paused")
	}
	return i.tmuxSession.SetDetachedSize(width, height)
}

// GetGitWorktree returns the git worktree for the instance
func (i *Instance) GetGitWorktree() (*git.GitWorktree, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot get git worktree for instance that has not been started")
	}
	return i.gitWorktree, nil
}

// GetWorktreePath returns the worktree path for the instance, or empty string if unavailable
func (i *Instance) GetWorktreePath() string {
	if i.gitWorktree == nil {
		return ""
	}
	return i.gitWorktree.GetWorktreePath()
}

func (i *Instance) Started() bool {
	return i.started
}

// SetTitle sets the title of the instance. Returns an error if the instance has started.
// We cant change the title once it's been used for a tmux session etc.
func (i *Instance) SetTitle(title string) error {
	if i.started {
		return fmt.Errorf("cannot change title of a started instance")
	}
	i.Title = title
	return nil
}

// claudeProjectDir returns the conversation history directory that the claude CLI
// uses for a given working directory. The encoding rule observed in ~/.claude/projects
// replaces every '/', '.', and '_' with '-'.
func claudeProjectDir(workingPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	encoded := workingPath
	encoded = strings.ReplaceAll(encoded, "/", "-")
	encoded = strings.ReplaceAll(encoded, ".", "-")
	encoded = strings.ReplaceAll(encoded, "_", "-")
	return filepath.Join(home, ".claude", "projects", encoded), nil
}

// rewriteCwdInClaudeDir rewrites every .jsonl file in dir, replacing all occurrences
// of oldPath with newPath. claude embeds the working directory both in per-message
// "cwd" metadata and in tool outputs, so a plain string substitution is what we
// need: after this runs, `claude --resume` no longer reports "different directory".
// Each file is updated atomically (write to .tmp + rename); files that don't
// reference oldPath are skipped.
func rewriteCwdInClaudeDir(dir, oldPath, newPath string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read claude project dir: %w", err)
	}
	old := []byte(oldPath)
	new := []byte(newPath)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(full)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", full, err)
		}
		if !bytes.Contains(b, old) {
			continue
		}
		rewritten := bytes.ReplaceAll(b, old, new)
		tmp := full + ".rename.tmp"
		if err := os.WriteFile(tmp, rewritten, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", tmp, err)
		}
		if err := os.Rename(tmp, full); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("failed to commit %s: %w", full, err)
		}
	}
	return nil
}

// Rename changes the title of a paused instance, also renaming the underlying git
// branch and (if present) the claude conversation history directory so that
// derived names stay consistent. The old tmux session is killed at the end —
// the next Resume starts a fresh session rooted at the new worktree path,
// avoiding the case where the inner shell/claude process keeps the deleted
// old worktree as its cwd. claude conversation history survives because the
// jsonl rewrite step updates per-entry cwd metadata.
//
// Only allowed when the instance is paused (worktree directory is absent,
// tmux is detached). All identifiers are computed up front so collisions can
// be rejected before any side effects are applied; failures during the apply
// phase trigger best-effort rollback of earlier steps.
func (i *Instance) Rename(newTitle string) error {
	newTitle = strings.TrimSpace(newTitle)
	if newTitle == "" {
		return fmt.Errorf("title cannot be empty")
	}
	if !i.started {
		return fmt.Errorf("cannot rename instance that has not been started")
	}
	if i.Status != Paused {
		return fmt.Errorf("instance must be paused to rename")
	}
	if newTitle == i.Title {
		return nil
	}

	// 1. Plan: compute all new identifiers up front so we can validate collisions
	//    without touching anything yet.
	newBranchName, newWorktreePath, err := git.PlanRename(newTitle)
	if err != nil {
		return err
	}

	oldWorktreePath := i.gitWorktree.GetWorktreePath()
	oldBranchName := i.gitWorktree.GetBranchName()

	oldClaudeDir, err := claudeProjectDir(oldWorktreePath)
	if err != nil {
		return err
	}
	newClaudeDir, err := claudeProjectDir(newWorktreePath)
	if err != nil {
		return err
	}

	// 2. Pre-checks. Fail fast before any side effect.
	if newBranchName != oldBranchName && i.gitWorktree.BranchExists(newBranchName) {
		return fmt.Errorf("branch %s already exists", newBranchName)
	}
	if probe := tmux.NewTmuxSession(newTitle, i.Program); probe.DoesSessionExist() {
		return fmt.Errorf("tmux session for %q already exists", newTitle)
	}

	claudeDirExists := false
	if _, statErr := os.Stat(oldClaudeDir); statErr == nil {
		claudeDirExists = true
		if _, dstErr := os.Stat(newClaudeDir); dstErr == nil {
			return fmt.Errorf("claude project directory already exists at %s", newClaudeDir)
		} else if !os.IsNotExist(dstErr) {
			return fmt.Errorf("failed to check claude project directory destination: %w", dstErr)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to check claude project directory: %w", statErr)
	}

	// 3. Apply. Tmux is closed last so earlier-step rollback doesn't have to
	//    recreate a session: branch rename → claude dir mv → jsonl rewrite →
	//    tmux close + replace.
	if err := i.gitWorktree.RenameBranch(newTitle, newBranchName, newWorktreePath); err != nil {
		return fmt.Errorf("failed to rename git branch: %w", err)
	}

	if claudeDirExists {
		if err := os.Rename(oldClaudeDir, newClaudeDir); err != nil {
			if rbErr := i.gitWorktree.RollbackBranchRename(oldBranchName, oldWorktreePath); rbErr != nil {
				log.ErrorLog.Printf("failed to roll back branch rename: %v", rbErr)
			}
			return fmt.Errorf("failed to rename claude project directory: %w", err)
		}
		// Rewrite cwd metadata inside the moved jsonl files so `claude --resume`
		// recognises the new worktree path. Without this step claude reports
		// "this conversation is from a different directory" because it trusts
		// the per-entry cwd field, not the directory name.
		if err := rewriteCwdInClaudeDir(newClaudeDir, oldWorktreePath, newWorktreePath); err != nil {
			if rbErr := os.Rename(newClaudeDir, oldClaudeDir); rbErr != nil {
				log.ErrorLog.Printf("failed to roll back claude project dir move: %v", rbErr)
			}
			if rbErr := i.gitWorktree.RollbackBranchRename(oldBranchName, oldWorktreePath); rbErr != nil {
				log.ErrorLog.Printf("failed to roll back branch rename: %v", rbErr)
			}
			return fmt.Errorf("failed to rewrite claude history cwd: %w", err)
		}
	}

	// Kill the old tmux session and replace the in-memory handle. The old shell/
	// claude process was holding the now-deleted old worktree as its cwd, so
	// keeping the session would only carry that broken state forward. A failure
	// here is non-fatal — the orphan session would be harmless and the next
	// Resume creates a fresh one regardless.
	if err := i.tmuxSession.Close(); err != nil {
		log.ErrorLog.Printf("failed to close old tmux session: %v", err)
	}
	i.tmuxSession = tmux.NewTmuxSession(newTitle, i.Program)

	// 4. Memory.
	i.Title = newTitle
	i.Branch = i.gitWorktree.GetBranchName()
	i.UpdatedAt = time.Now()
	return nil
}

func (i *Instance) Paused() bool {
	return i.Status == Paused
}

// TmuxAlive returns true if the tmux session is alive. This is a sanity check before attaching.
func (i *Instance) TmuxAlive() bool {
	return i.tmuxSession.DoesSessionExist()
}

// Pause stops the tmux session and removes the worktree, preserving the branch
func (i *Instance) Pause() error {
	if !i.started {
		return fmt.Errorf("cannot pause instance that has not been started")
	}
	if i.Status == Paused {
		return fmt.Errorf("instance is already paused")
	}

	copyInstanceName := config.LoadConfig().ShouldCopyInstanceNameOnCheckout()

	var errs []error

	// If the worktree is orphaned (path or .git missing), git cannot operate
	// on it. Skip dirty check and Remove, prune any lingering metadata, then
	// transition to Paused so the user can recover via Resume.
	if valid, err := i.gitWorktree.IsValidWorktree(); err != nil {
		errs = append(errs, fmt.Errorf("failed to validate worktree: %w", err))
		log.ErrorLog.Print(err)
	} else if !valid {
		log.WarningLog.Printf("worktree at %s is orphaned; skipping dirty check and remove",
			i.gitWorktree.GetWorktreePath())
		if err := i.tmuxSession.DetachSafely(); err != nil {
			errs = append(errs, fmt.Errorf("failed to detach tmux session: %w", err))
			log.ErrorLog.Print(err)
		}
		// Drop any leftover directory so a future Resume's `git worktree add` won't conflict.
		if err := os.RemoveAll(i.gitWorktree.GetWorktreePath()); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove orphaned worktree directory: %w", err))
			log.ErrorLog.Print(err)
		}
		if err := i.gitWorktree.Prune(); err != nil {
			errs = append(errs, fmt.Errorf("failed to prune git worktrees: %w", err))
			log.ErrorLog.Print(err)
		}
		i.SetStatus(Paused)
		if copyInstanceName {
			_ = clipboard.WriteAll(i.gitWorktree.GetBranchName())
		}
		return i.combineErrors(errs)
	}

	// Check if there are any changes to commit
	if dirty, err := i.gitWorktree.IsDirty(); err != nil {
		errs = append(errs, fmt.Errorf("failed to check if worktree is dirty: %w", err))
		log.ErrorLog.Print(err)
	} else if dirty {
		// Commit changes locally (without pushing to GitHub)
		commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s (paused)", i.Title, time.Now().Format(time.RFC822))
		if err := i.gitWorktree.CommitChanges(commitMsg); err != nil {
			errs = append(errs, fmt.Errorf("failed to commit changes: %w", err))
			log.ErrorLog.Print(err)
			// Return early if we can't commit changes to avoid corrupted state
			return i.combineErrors(errs)
		}
	}

	// Detach from tmux session instead of closing to preserve session output
	if err := i.tmuxSession.DetachSafely(); err != nil {
		errs = append(errs, fmt.Errorf("failed to detach tmux session: %w", err))
		log.ErrorLog.Print(err)
		// Continue with pause process even if detach fails
	}

	// Check if worktree exists before trying to remove it
	if _, err := os.Stat(i.gitWorktree.GetWorktreePath()); err == nil {
		// Remove worktree but keep branch
		if err := i.gitWorktree.Remove(); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove git worktree: %w", err))
			log.ErrorLog.Print(err)
			return i.combineErrors(errs)
		}

		// Only prune if remove was successful
		if err := i.gitWorktree.Prune(); err != nil {
			errs = append(errs, fmt.Errorf("failed to prune git worktrees: %w", err))
			log.ErrorLog.Print(err)
			return i.combineErrors(errs)
		}
	}

	i.SetStatus(Paused)
	if copyInstanceName {
		_ = clipboard.WriteAll(i.gitWorktree.GetBranchName())
	}

	if err := i.combineErrors(errs); err != nil {
		log.ErrorLog.Print(err)
		return err
	}
	return nil
}

// Resume recreates the worktree and restarts the tmux session
func (i *Instance) Resume() error {
	if !i.started {
		return fmt.Errorf("cannot resume instance that has not been started")
	}
	if i.Status != Paused {
		return fmt.Errorf("can only resume paused instances")
	}

	// Check if branch is checked out
	if checked, err := i.gitWorktree.IsBranchCheckedOut(); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to check if branch is checked out: %w", err)
	} else if checked {
		return fmt.Errorf("cannot resume: branch is checked out, please switch to a different branch")
	}

	// Setup git worktree
	if err := i.gitWorktree.Setup(); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to setup git worktree: %w", err)
	}

	// Check if tmux session still exists from pause, otherwise create new one
	if i.tmuxSession.DoesSessionExist() {
		// Session exists, just restore PTY connection to it
		if err := i.tmuxSession.Restore(); err != nil {
			log.ErrorLog.Print(err)
			// If restore fails, fall back to creating new session
			if err := i.tmuxSession.Start(i.gitWorktree.GetWorktreePath()); err != nil {
				log.ErrorLog.Print(err)
				// Cleanup git worktree if tmux session creation fails
				if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
					err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
					log.ErrorLog.Print(err)
				}
				return fmt.Errorf("failed to start new session: %w", err)
			}
		}
	} else {
		// Create new tmux session
		if err := i.tmuxSession.Start(i.gitWorktree.GetWorktreePath()); err != nil {
			log.ErrorLog.Print(err)
			// Cleanup git worktree if tmux session creation fails
			if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
				log.ErrorLog.Print(err)
			}
			return fmt.Errorf("failed to start new session: %w", err)
		}
	}

	i.SetStatus(Running)
	return nil
}

// RestartInstance fully restarts the selected instance: dirty changes are
// committed, the worktree is removed, the tmux session (and its child claude
// process) is killed, then everything is recreated from scratch. The git
// branch is preserved.
//
// This is implemented as Pause → tmux.Close → Resume because Pause normally
// only *detaches* the tmux session (keeping the inner claude process alive);
// the explicit Close in between is what forces a fresh program launch on
// Resume.
func (i *Instance) RestartInstance() error {
	if !i.started {
		return fmt.Errorf("cannot restart: instance has not been started yet")
	}
	if i.Status == Paused {
		return fmt.Errorf("cannot restart: instance is paused (resume it first with 'r')")
	}

	if err := i.Pause(); err != nil {
		return fmt.Errorf("failed to pause during restart: %w", err)
	}

	// Pause leaves tmux detached but alive. Close it now so Resume spawns a
	// fresh tmux + claude rather than reattaching to the old one.
	if err := i.tmuxSession.Close(); err != nil {
		log.ErrorLog.Printf("failed to close tmux session during restart: %v", err)
	}

	if err := i.Resume(); err != nil {
		return fmt.Errorf("failed to resume during restart: %w", err)
	}
	return nil
}

// UpdateDiffStats updates the git diff statistics for this instance
func (i *Instance) UpdateDiffStats() error {
	if !i.started {
		i.diffStats = nil
		return nil
	}

	if i.Status == Paused {
		// Keep the previous diff stats if the instance is paused
		return nil
	}

	stats := i.gitWorktree.Diff()
	if stats.Error != nil {
		if strings.Contains(stats.Error.Error(), "base commit SHA not set") {
			// Worktree is not fully set up yet, not an error
			i.diffStats = nil
			return nil
		}
		return fmt.Errorf("failed to get diff stats: %w", stats.Error)
	}

	i.diffStats = stats
	return nil
}

// ComputeDiff runs the expensive git diff I/O and returns the result without
// mutating instance state. Safe to call from a background goroutine.
func (i *Instance) ComputeDiff() *git.DiffStats {
	if !i.started || i.Status == Paused {
		return nil
	}
	return i.gitWorktree.Diff()
}

// ComputeDiffNumstat runs a lightweight git diff --numstat and returns only the
// added/removed line counts (Content is left empty). Safe to call from a
// background goroutine. Use this for instances whose full diff content is not
// currently needed so we avoid keeping large diffs in memory.
func (i *Instance) ComputeDiffNumstat() *git.DiffStats {
	if !i.started || i.Status == Paused {
		return nil
	}
	return i.gitWorktree.DiffNumstat()
}

// SetDiffStats sets the diff statistics on the instance. Should be called from
// the main event loop to avoid data races with View.
func (i *Instance) SetDiffStats(stats *git.DiffStats) {
	i.diffStats = stats
}

// GetDiffStats returns the current git diff statistics
func (i *Instance) GetDiffStats() *git.DiffStats {
	return i.diffStats
}

// SendPrompt sends a prompt to the tmux session
func (i *Instance) SendPrompt(prompt string) error {
	if !i.started {
		return fmt.Errorf("instance not started")
	}
	if i.tmuxSession == nil {
		return fmt.Errorf("tmux session not initialized")
	}
	if err := i.tmuxSession.SendKeys(prompt); err != nil {
		return fmt.Errorf("error sending keys to tmux session: %w", err)
	}

	// Brief pause to prevent carriage return from being interpreted as newline
	time.Sleep(100 * time.Millisecond)
	if err := i.tmuxSession.TapEnter(); err != nil {
		return fmt.Errorf("error tapping enter: %w", err)
	}

	return nil
}

// PreviewFullHistory captures the entire tmux pane output including full scrollback history
func (i *Instance) PreviewFullHistory() (string, error) {
	if !i.started || i.Status == Paused {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContentWithOptions("-", "-")
}

// SetTmuxSession sets the tmux session for testing purposes
func (i *Instance) SetTmuxSession(session *tmux.TmuxSession) {
	i.tmuxSession = session
}

// SendKeys sends keys to the tmux session
func (i *Instance) SendKeys(keys string) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot send keys to instance that has not been started or is paused")
	}
	return i.tmuxSession.SendKeys(keys)
}
