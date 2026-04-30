package config

import (
	"claude-squad/log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain runs before all tests to set up the test environment
func TestMain(m *testing.M) {
	// Initialize the logger before any tests run
	log.Initialize(false)
	defer log.Close()

	exitCode := m.Run()
	os.Exit(exitCode)
}

// setupTempRepo creates an empty git repo in a temp dir and chdir's into it.
// Returns the canonical path to the repo. Cwd is restored at test end.
func setupTempRepo(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err)

	require.NoError(t, exec.Command("git", "init", resolved).Run())

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(resolved))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	return resolved
}

func TestGetClaudeCommand(t *testing.T) {
	originalShell := os.Getenv("SHELL")
	originalPath := os.Getenv("PATH")
	defer func() {
		os.Setenv("SHELL", originalShell)
		os.Setenv("PATH", originalPath)
	}()

	t.Run("finds claude in PATH", func(t *testing.T) {
		// Create a temporary directory with a mock claude executable
		tempDir := t.TempDir()
		claudePath := filepath.Join(tempDir, "claude")

		// Create a mock executable
		err := os.WriteFile(claudePath, []byte("#!/bin/bash\necho 'mock claude'"), 0755)
		require.NoError(t, err)

		// Set PATH to include our temp directory
		os.Setenv("PATH", tempDir+":"+originalPath)
		os.Setenv("SHELL", "/bin/bash")

		result, err := GetClaudeCommand()

		assert.NoError(t, err)
		assert.True(t, strings.Contains(result, "claude"))
	})

	t.Run("handles missing claude command", func(t *testing.T) {
		// Set PATH to a directory that doesn't contain claude
		tempDir := t.TempDir()
		os.Setenv("PATH", tempDir)
		os.Setenv("SHELL", "/bin/bash")

		result, err := GetClaudeCommand()

		assert.Error(t, err)
		assert.Equal(t, "", result)
		assert.Contains(t, err.Error(), "claude command not found")
	})

	t.Run("handles empty SHELL environment", func(t *testing.T) {
		// Create a temporary directory with a mock claude executable
		tempDir := t.TempDir()
		claudePath := filepath.Join(tempDir, "claude")

		// Create a mock executable
		err := os.WriteFile(claudePath, []byte("#!/bin/bash\necho 'mock claude'"), 0755)
		require.NoError(t, err)

		// Set PATH and unset SHELL
		os.Setenv("PATH", tempDir+":"+originalPath)
		os.Unsetenv("SHELL")

		result, err := GetClaudeCommand()

		assert.NoError(t, err)
		assert.True(t, strings.Contains(result, "claude"))
	})

	t.Run("handles alias parsing", func(t *testing.T) {
		// Test core alias formats
		aliasRegex := regexp.MustCompile(`(?:aliased to|->|=)\s*([^\s]+)`)

		// Standard alias format
		output := "claude: aliased to /usr/local/bin/claude"
		matches := aliasRegex.FindStringSubmatch(output)
		assert.Len(t, matches, 2)
		assert.Equal(t, "/usr/local/bin/claude", matches[1])

		// Direct path (no alias)
		output = "/usr/local/bin/claude"
		matches = aliasRegex.FindStringSubmatch(output)
		assert.Len(t, matches, 0)
	})
}

func TestDefaultConfig(t *testing.T) {
	t.Run("creates config with default values", func(t *testing.T) {
		config := DefaultConfig()

		assert.NotNil(t, config)
		assert.NotEmpty(t, config.DefaultProgram)
		assert.False(t, config.AutoYes)
		assert.Equal(t, 1000, config.DaemonPollInterval)
		assert.NotEmpty(t, config.BranchPrefix)
		assert.True(t, strings.HasSuffix(config.BranchPrefix, "/"))
	})

}

func TestGetRepoConfigDir(t *testing.T) {
	t.Run("returns <repo>/.claude-squad inside a git repo", func(t *testing.T) {
		repo := setupTempRepo(t)

		configDir, err := GetRepoConfigDir()

		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(repo, ".claude-squad"), configDir)
		assert.True(t, filepath.IsAbs(configDir))
	})

	t.Run("errors when not in a git repo", func(t *testing.T) {
		tempDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		origWD, err := os.Getwd()
		require.NoError(t, err)
		require.NoError(t, os.Chdir(tempDir))
		t.Cleanup(func() { _ = os.Chdir(origWD) })

		_, err = GetRepoConfigDir()
		assert.Error(t, err)
	})
}

func TestLoadConfig(t *testing.T) {
	t.Run("returns default config when file doesn't exist", func(t *testing.T) {
		setupTempRepo(t)

		config := LoadConfig()

		assert.NotNil(t, config)
		assert.NotEmpty(t, config.DefaultProgram)
		assert.False(t, config.AutoYes)
		assert.Equal(t, 1000, config.DaemonPollInterval)
		assert.NotEmpty(t, config.BranchPrefix)
	})

	t.Run("loads valid config file", func(t *testing.T) {
		repo := setupTempRepo(t)
		configDir := filepath.Join(repo, ".claude-squad")
		require.NoError(t, os.MkdirAll(configDir, 0755))

		configPath := filepath.Join(configDir, ConfigFileName)
		configContent := `{
			"default_program": "test-claude",
			"auto_yes": true,
			"daemon_poll_interval": 2000,
			"branch_prefix": "test/"
		}`
		require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

		config := LoadConfig()

		assert.NotNil(t, config)
		assert.Equal(t, "test-claude", config.DefaultProgram)
		assert.True(t, config.AutoYes)
		assert.Equal(t, 2000, config.DaemonPollInterval)
		assert.Equal(t, "test/", config.BranchPrefix)
	})

	t.Run("returns default config on invalid JSON", func(t *testing.T) {
		repo := setupTempRepo(t)
		configDir := filepath.Join(repo, ".claude-squad")
		require.NoError(t, os.MkdirAll(configDir, 0755))

		configPath := filepath.Join(configDir, ConfigFileName)
		require.NoError(t, os.WriteFile(configPath, []byte(`{"invalid": json content}`), 0644))

		config := LoadConfig()

		assert.NotNil(t, config)
		assert.NotEmpty(t, config.DefaultProgram)
		assert.False(t, config.AutoYes)
		assert.Equal(t, 1000, config.DaemonPollInterval)
	})
}

func TestGetProgram(t *testing.T) {
	t.Run("no profiles returns default_program as-is", func(t *testing.T) {
		cfg := &Config{DefaultProgram: "/usr/local/bin/claude"}
		assert.Equal(t, "/usr/local/bin/claude", cfg.GetProgram())
	})

	t.Run("profiles defined and default_program matches a profile name", func(t *testing.T) {
		cfg := &Config{
			DefaultProgram: "claude",
			Profiles: []Profile{
				{Name: "claude", Program: "/usr/local/bin/claude"},
				{Name: "aider", Program: "aider --model ollama_chat/gemma3:1b"},
			},
		}
		assert.Equal(t, "/usr/local/bin/claude", cfg.GetProgram())
	})

	t.Run("profiles defined but default_program does not match any profile", func(t *testing.T) {
		cfg := &Config{
			DefaultProgram: "some-other-program",
			Profiles: []Profile{
				{Name: "claude", Program: "/usr/local/bin/claude"},
			},
		}
		assert.Equal(t, "some-other-program", cfg.GetProgram())
	})
}

func TestGetProfiles(t *testing.T) {
	t.Run("no profiles returns single synthetic profile", func(t *testing.T) {
		cfg := &Config{DefaultProgram: "/usr/local/bin/claude"}
		profiles := cfg.GetProfiles()
		assert.Len(t, profiles, 1)
		assert.Equal(t, "/usr/local/bin/claude", profiles[0].Name)
		assert.Equal(t, "/usr/local/bin/claude", profiles[0].Program)
	})

	t.Run("profiles defined returns them with default first", func(t *testing.T) {
		cfg := &Config{
			DefaultProgram: "aider",
			Profiles: []Profile{
				{Name: "claude", Program: "/usr/local/bin/claude"},
				{Name: "aider", Program: "aider --model gemma"},
			},
		}
		profiles := cfg.GetProfiles()
		assert.Len(t, profiles, 2)
		assert.Equal(t, "aider", profiles[0].Name)
		assert.Equal(t, "claude", profiles[1].Name)
	})

	t.Run("profiles defined but default not matching preserves order", func(t *testing.T) {
		cfg := &Config{
			DefaultProgram: "other",
			Profiles: []Profile{
				{Name: "claude", Program: "/usr/local/bin/claude"},
				{Name: "aider", Program: "aider --model gemma"},
			},
		}
		profiles := cfg.GetProfiles()
		assert.Len(t, profiles, 2)
		assert.Equal(t, "claude", profiles[0].Name)
		assert.Equal(t, "aider", profiles[1].Name)
	})
}

func TestSaveConfig(t *testing.T) {
	t.Run("saves config to file under <repo>/.claude-squad", func(t *testing.T) {
		repo := setupTempRepo(t)

		testConfig := &Config{
			DefaultProgram:     "test-program",
			AutoYes:            true,
			DaemonPollInterval: 3000,
			BranchPrefix:       "test-branch/",
		}

		err := SaveConfig(testConfig)
		assert.NoError(t, err)

		configPath := filepath.Join(repo, ".claude-squad", ConfigFileName)
		assert.FileExists(t, configPath)

		loadedConfig := LoadConfig()
		assert.Equal(t, testConfig.DefaultProgram, loadedConfig.DefaultProgram)
		assert.Equal(t, testConfig.AutoYes, loadedConfig.AutoYes)
		assert.Equal(t, testConfig.DaemonPollInterval, loadedConfig.DaemonPollInterval)
		assert.Equal(t, testConfig.BranchPrefix, loadedConfig.BranchPrefix)
	})

	t.Run("first save adds .claude-squad/ to .gitignore", func(t *testing.T) {
		repo := setupTempRepo(t)

		err := SaveConfig(DefaultConfig())
		require.NoError(t, err)

		gitignore, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
		require.NoError(t, err)
		assert.Contains(t, string(gitignore), ".claude-squad/")
	})
}
