package keys

import (
	"github.com/charmbracelet/bubbles/key"
)

type KeyName int

const (
	KeyUp KeyName = iota
	KeyDown
	KeyEnter
	KeyNew
	KeyKill
	KeyQuit
	KeyReview
	KeyPush
	KeySubmit

	KeyTab        // Tab is a special keybinding for switching between panes.
	KeySubmitName // SubmitName is a special keybinding for submitting the name of a new instance.

	KeyCheckout
	KeyResume
	KeyRename          // Rename a paused instance
	KeyPrompt          // New key for entering a prompt
	KeyHelp            // Key for showing help screen
	KeyAttachExternal  // Open a new OS terminal window attached to the tmux session
	KeyRestartInstance // Restart the selected instance (commit, kill tmux+claude, restart fresh)

	// Diff keybindings
	KeyShiftUp
	KeyShiftDown

	// Reorder keybindings
	KeyMoveUp
	KeyMoveDown

	// Search
	KeySearch
)

// GlobalKeyStringsMap is a global, immutable map string to keybinding.
var GlobalKeyStringsMap = map[string]KeyName{
	"up":         KeyUp,
	"k":          KeyUp,
	"down":       KeyDown,
	"j":          KeyDown,
	"shift+up":   KeyShiftUp,
	"shift+down": KeyShiftDown,
	"J":          KeyMoveDown,
	"K":          KeyMoveUp,
	"N":          KeyPrompt,
	"enter":      KeyEnter,
	"o":          KeyEnter,
	"n":          KeyNew,
	"D":          KeyKill,
	"q":          KeyQuit,
	"tab":        KeyTab,
	"c":          KeyCheckout,
	"r":          KeyResume,
	"R":          KeyRename,
	"p":          KeySubmit,
	"?":          KeyHelp,
	"A":          KeyAttachExternal,
	"S":          KeyRestartInstance,
	"/":          KeySearch,

	// Korean 두벌식 자모. Shift 변형이 없는 자모는 일반 키 동작만 매핑 (Shift 동작은 한글 모드에서 포기).
	"ㅂ": KeyQuit,
	"ㅓ": KeyDown, // shift+j도 ㅓ → KeyMoveDown 포기
	"ㅏ": KeyUp,   // shift+k도 ㅏ → KeyMoveUp 포기
	"ㅊ": KeyCheckout,
	"ㄱ": KeyResume,
	"ㄲ": KeyRename,
	"ㅔ": KeySubmit,
	"ㅐ": KeyEnter,
	"ㅁ": KeyAttachExternal,
	"ㄴ": KeyRestartInstance,
	"ㅇ": KeyKill,
	"ㅜ": KeyNew, // shift+n도 ㅜ → KeyPrompt 포기
}

// GlobalkeyBindings is a global, immutable map of KeyName tot keybinding.
var GlobalkeyBindings = map[KeyName]key.Binding{
	KeyUp: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	KeyDown: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	KeyShiftUp: key.NewBinding(
		key.WithKeys("shift+up"),
		key.WithHelp("shift+↑", "scroll"),
	),
	KeyShiftDown: key.NewBinding(
		key.WithKeys("shift+down"),
		key.WithHelp("shift+↓", "scroll"),
	),
	KeyEnter: key.NewBinding(
		key.WithKeys("enter", "o"),
		key.WithHelp("↵/o", "open"),
	),
	KeyNew: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new"),
	),
	KeyKill: key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "kill"),
	),
	KeyHelp: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	KeyQuit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	KeySubmit: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "push branch"),
	),
	KeyPrompt: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "new with prompt"),
	),
	KeyCheckout: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "checkout"),
	),
	KeyTab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch tab"),
	),
	KeyResume: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "resume"),
	),
	KeyRename: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "rename"),
	),

	KeyMoveUp: key.NewBinding(
		key.WithKeys("K"),
		key.WithHelp("K", "move up"),
	),
	KeyMoveDown: key.NewBinding(
		key.WithKeys("J"),
		key.WithHelp("J", "move down"),
	),
	KeyAttachExternal: key.NewBinding(
		key.WithKeys("A"),
		key.WithHelp("A", "attach in new window"),
	),
	KeyRestartInstance: key.NewBinding(
		key.WithKeys("S"),
		key.WithHelp("S", "restart"),
	),
	KeySearch: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),

	// -- Special keybindings --

	KeySubmitName: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit name"),
	),
}
