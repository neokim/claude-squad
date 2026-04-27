// Package notify dispatches OS-level desktop notifications.
package notify

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Send displays a desktop notification with the given title and body.
// Errors are returned but typically logged rather than surfaced — a failed
// notification should never break the foreground TUI.
func Send(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		return sendDarwin(title, body)
	case "linux":
		return sendLinux(title, body)
	case "windows":
		return sendWindows(title, body)
	default:
		return fmt.Errorf("notifications unsupported on %s", runtime.GOOS)
	}
}

func sendDarwin(title, body string) error {
	script := fmt.Sprintf(
		`display notification %q with title %q sound name "Glass"`,
		appleScriptEscape(body), appleScriptEscape(title),
	)
	return exec.Command("osascript", "-e", script).Run()
}

func sendLinux(title, body string) error {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return fmt.Errorf("notify-send not found: %w", err)
	}
	return exec.Command("notify-send", title, body).Run()
}

func sendWindows(title, body string) error {
	script := fmt.Sprintf(
		`[reflection.assembly]::loadwithpartialname("System.Windows.Forms") | Out-Null; `+
			`[reflection.assembly]::loadwithpartialname("System.Drawing") | Out-Null; `+
			`$n = New-Object System.Windows.Forms.NotifyIcon; `+
			`$n.Icon = [System.Drawing.SystemIcons]::Information; `+
			`$n.Visible = $true; `+
			`$n.ShowBalloonTip(5000, %q, %q, [System.Windows.Forms.ToolTipIcon]::Info)`,
		title, body,
	)
	return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
}

// appleScriptEscape escapes characters that would terminate or break out of an
// AppleScript string literal: backslash and double quote.
func appleScriptEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
