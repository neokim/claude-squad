package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type ErrBox struct {
	height, width int
	err           error
}

var errStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#FF0000",
	Dark:  "#FF0000",
})

func NewErrBox() *ErrBox {
	return &ErrBox{}
}

func (e *ErrBox) SetError(err error) {
	e.err = err
}

func (e *ErrBox) Clear() {
	e.err = nil
}

func (e *ErrBox) SetSize(width, height int) {
	e.width = width
	e.height = height
}

// Width returns the configured width of the box. Used by callers that want to
// render a same-width status line in place of the error.
func (e *ErrBox) Width() int {
	return e.width
}

func (e *ErrBox) String() string {
	var err string
	if e.err != nil {
		err = e.err.Error()
		lines := strings.Split(err, "\n")
		err = strings.Join(lines, "//")
		if runewidth.StringWidth(err) > e.width-3 && e.width-3 >= 0 {
			err = runewidth.Truncate(err, e.width-3, "...")
		}
	}
	return lipgloss.Place(e.width, e.height, lipgloss.Center, lipgloss.Center, errStyle.Render(err))
}
