package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	userHomeConfigDirName = ".claude-squad"
	repoConfigDirName     = ".claude-squad"
	worktreesSubdir       = "worktrees"
	trashSubdir           = "trash"
)

// GetUserHomeConfigDir returns ~/.claude-squad. Currently used only as the
// parent of the worktrees/ subtree; per-repo config lives under GetRepoConfigDir.
func GetUserHomeConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, userHomeConfigDirName), nil
}

// GetRepoRoot returns the absolute path of the main repository containing the
// current working directory. When invoked from a linked git worktree, it
// resolves to the parent repository so all worktrees of the same repo share
// state. Errors if cwd is not inside a git repository.
func GetRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return resolveRepoRoot(cwd)
}

// resolveRepoRoot finds the main repo root for an arbitrary path. It uses
// --git-common-dir (which returns the main repo's .git even from a linked
// worktree) and strips the trailing /.git component.
func resolveRepoRoot(path string) (string, error) {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %s", path)
	}
	gitCommonDir := strings.TrimSpace(string(out))

	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(path, gitCommonDir)
	}
	abs, err := filepath.Abs(gitCommonDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve git common dir: %w", err)
	}
	return filepath.Dir(abs), nil
}

// GetRepoID returns a stable identifier for a repo: <basename>_<sha256(abs_path)[:8]>.
// The basename is human-readable; the hash prevents collisions across repos
// that share a name.
func GetRepoID(repoRoot string) string {
	base := filepath.Base(repoRoot)
	h := sha256.Sum256([]byte(repoRoot))
	return fmt.Sprintf("%s_%s", base, hex.EncodeToString(h[:])[:8])
}

// GetRepoConfigDir returns <repo-root>/.claude-squad. All per-repo state
// (config.json, state.json, daemon.pid) lives here.
func GetRepoConfigDir() (string, error) {
	root, err := GetRepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, repoConfigDirName), nil
}

// GetWorktreesBaseDir returns ~/.claude-squad/worktrees/<repo-id>/. Worktrees
// are kept outside the source repo because nesting confuses git (status,
// clean) and IDE indexing.
func GetWorktreesBaseDir() (string, error) {
	home, err := GetUserHomeConfigDir()
	if err != nil {
		return "", err
	}
	root, err := GetRepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, worktreesSubdir, GetRepoID(root)), nil
}

// GetTrashDir returns ~/.claude-squad/trash. Worktrees being discarded are
// renamed into here so the (potentially very slow) recursive delete can happen
// off the critical path. It is a sibling of worktrees/ — same filesystem, so
// the rename is atomic — but outside it, so worktree enumeration ignores it.
func GetTrashDir() (string, error) {
	home, err := GetUserHomeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, trashSubdir), nil
}

// EnsureRepoConfigDir creates the per-repo config dir and, on first creation,
// best-effort adds .claude-squad/ to the repo's .gitignore. Returns the path.
func EnsureRepoConfigDir() (string, error) {
	dir, err := GetRepoConfigDir()
	if err != nil {
		return "", err
	}
	created := false
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		created = true
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create repo config dir: %w", err)
	}
	if created {
		_ = ensureGitignoreEntry()
	}
	return dir, nil
}

// ensureGitignoreEntry appends ".claude-squad/" to the repo's .gitignore if no
// matching entry exists. Comments and blank lines are skipped; both
// ".claude-squad" and ".claude-squad/" (with optional leading slash) are
// recognized as already-present.
func ensureGitignoreEntry() error {
	root, err := GetRepoRoot()
	if err != nil {
		return err
	}
	gitignorePath := filepath.Join(root, ".gitignore")

	existing, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if hasGitignoreEntry(string(existing), repoConfigDirName) {
		return nil
	}

	var sb strings.Builder
	sb.Write(existing)
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		sb.WriteByte('\n')
	}
	sb.WriteString(repoConfigDirName + "/\n")
	return os.WriteFile(gitignorePath, []byte(sb.String()), 0644)
}

func hasGitignoreEntry(content, name string) bool {
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cleaned := strings.TrimPrefix(line, "/")
		cleaned = strings.TrimSuffix(cleaned, "/")
		if cleaned == name {
			return true
		}
	}
	return false
}
