package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"os"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	log.Initialize(false)
	defer log.Close()
	os.Exit(m.Run())
}

// newTestList creates a list with n mock items and a given height.
// Each item renders to 4 lines (title padding + title + desc + desc padding).
func newTestList(n int, height int) *List {
	s := spinner.New()
	l := NewList(&s, false)
	l.SetSize(60, height)

	for i := 0; i < n; i++ {
		inst := &session.Instance{Title: "test"}
		l.items = append(l.items, inst)
	}
	return l
}

// newTestListWithTitles creates a list with named active instances for reorder tests.
func newTestListWithTitles(titles ...string) *List {
	return newTestListWithGroups(titles)
}

// newTestListWithGroups builds a list whose items match the given titles, marking the ones in
// pausedTitles as paused. The order given is the order stored.
func newTestListWithGroups(titles []string, pausedTitles ...string) *List {
	paused := make(map[string]bool, len(pausedTitles))
	for _, t := range pausedTitles {
		paused[t] = true
	}
	s := spinner.New()
	l := NewList(&s, false)
	for _, t := range titles {
		inst, _ := session.NewInstance(session.InstanceOptions{
			Title:   t,
			Path:    ".",
			Program: "echo",
		})
		if paused[t] {
			inst.Status = session.Paused
		}
		l.items = append(l.items, inst)
	}
	return l
}

// visibleItemCount renders the list and counts how many items appear.
func visibleItemCount(l *List) int {
	rendered := renderItems(l)
	if len(rendered) == 0 {
		return 0
	}

	headerLines := 4
	availableLines := l.height - headerLines
	if availableLines < 1 {
		return 0
	}

	count := 0
	linesUsed := 0
	for i := l.scrollOffset; i < len(rendered); i++ {
		needed := rendered[i].lines
		if i > l.scrollOffset {
			needed += 1
		}
		if linesUsed+needed > availableLines && i > l.scrollOffset {
			break
		}
		linesUsed += needed
		count++
	}
	return count
}

// renderItems mirrors the rendering logic to get item line counts.
func renderItems(l *List) []listRenderedItem {
	rendered := make([]listRenderedItem, len(l.items))
	for i, item := range l.items {
		text := l.renderer.Render(item, i+1, i == l.selectedIdx, len(l.repos) > 1, l.searchQuery)
		lineCount := len(splitLines(text))
		rendered[i] = listRenderedItem{text: text, lines: lineCount}
	}
	return rendered
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}

func TestScrollDownAndUp(t *testing.T) {
	// Height 30 should fit about 5 items (4 lines each + 1 separator).
	l := newTestList(10, 30)

	// Initially at top.
	assert.Equal(t, 0, l.scrollOffset)
	assert.Equal(t, 0, l.selectedIdx)

	// Navigate down past visible area.
	for i := 0; i < 9; i++ {
		l.Down()
	}
	assert.Equal(t, 9, l.selectedIdx)

	// Scroll offset should have moved to keep selection visible.
	_ = l.String() // triggers adjustScrollOffset
	assert.Greater(t, l.scrollOffset, 0, "scroll offset should increase when navigating down")

	// Navigate back to top.
	for i := 0; i < 9; i++ {
		l.Up()
	}
	assert.Equal(t, 0, l.selectedIdx)
	_ = l.String()
	assert.Equal(t, 0, l.scrollOffset, "scroll offset should return to 0 when at top")
}

func TestScrollConsistentItemCount(t *testing.T) {
	// Use enough items and height so scrolling is needed.
	l := newTestList(20, 50)

	// Scroll to bottom.
	for i := 0; i < 19; i++ {
		l.Down()
	}
	_ = l.String()
	countAtBottom := visibleItemCount(l)

	// Scroll back to top.
	for i := 0; i < 19; i++ {
		l.Up()
	}
	_ = l.String()
	countAtTop := visibleItemCount(l)

	assert.Equal(t, countAtTop, countAtBottom,
		"visible item count should be the same at top and bottom")
}

func TestScrollAfterKillLastItem(t *testing.T) {
	l := newTestList(15, 50)

	// Scroll to the last item.
	for i := 0; i < 14; i++ {
		l.Down()
	}
	_ = l.String()
	assert.Equal(t, 14, l.selectedIdx)
	offsetBefore := l.scrollOffset

	// Kill the last item — should not leave empty space at bottom.
	// We can't call Kill() directly (it calls instance.Kill()), so simulate it.
	l.items = append(l.items[:l.selectedIdx], l.items[l.selectedIdx+1:]...)
	l.Up()
	if l.scrollOffset >= len(l.items) {
		l.scrollOffset = len(l.items) - 1
	}
	l.clampScrollOffset()

	_ = l.String()
	assert.Equal(t, 13, l.selectedIdx)
	assert.LessOrEqual(t, l.scrollOffset, offsetBefore,
		"scroll offset should decrease after deleting the last item")

	// Verify no excessive empty space: visible items should reach the end of the list.
	rendered := renderItems(l)
	headerLines := 4
	availableLines := l.height - headerLines
	linesUsed := 0
	lastVisible := l.scrollOffset
	for i := l.scrollOffset; i < len(rendered); i++ {
		needed := rendered[i].lines
		if i > l.scrollOffset {
			needed += 1
		}
		if linesUsed+needed > availableLines && i > l.scrollOffset {
			break
		}
		lastVisible = i
		linesUsed += needed
	}
	assert.Equal(t, len(l.items)-1, lastVisible,
		"last item should be visible after deleting from bottom")
}

func TestScrollOffsetBounds(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		l := newTestList(0, 50)
		l.Down()
		l.Up()
		assert.Equal(t, 0, l.scrollOffset)
		assert.Equal(t, 0, l.selectedIdx)
	})

	t.Run("items fit without scrolling", func(t *testing.T) {
		// 3 items in a large area — no scroll needed.
		l := newTestList(3, 80)
		l.Down()
		l.Down()
		_ = l.String()
		assert.Equal(t, 0, l.scrollOffset, "should not scroll when all items fit")
	})

	t.Run("scroll offset clamps after multiple deletions", func(t *testing.T) {
		l := newTestList(10, 30)
		// Navigate to the end.
		for i := 0; i < 9; i++ {
			l.Down()
		}
		_ = l.String()

		// Delete items from the end.
		for len(l.items) > 3 {
			l.items = l.items[:len(l.items)-1]
			if l.selectedIdx >= len(l.items) {
				l.selectedIdx = len(l.items) - 1
			}
			if l.scrollOffset >= len(l.items) {
				l.scrollOffset = len(l.items) - 1
			}
			l.clampScrollOffset()
		}
		_ = l.String()

		assert.Equal(t, 0, l.scrollOffset,
			"scroll offset should be 0 when remaining items fit on screen")
	})
}

func TestMoveUp(t *testing.T) {
	l := newTestListWithTitles("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveUp()
	require.True(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "b", l.items[0].Title)
	require.Equal(t, "a", l.items[1].Title)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveUp_AtTop_WrapsToBottom(t *testing.T) {
	l := newTestListWithTitles("a", "b", "c")
	l.SetSelectedInstance(0)

	moved := l.MoveUp()
	require.True(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "b", l.items[0].Title)
	require.Equal(t, "c", l.items[1].Title)
	require.Equal(t, "a", l.items[2].Title)
}

func TestMoveDown(t *testing.T) {
	l := newTestListWithTitles("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveDown()
	require.True(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
	require.Equal(t, "c", l.items[1].Title)
	require.Equal(t, "b", l.items[2].Title)
}

func TestMoveDown_AtBottom_WrapsToTop(t *testing.T) {
	l := newTestListWithTitles("a", "b", "c")
	l.SetSelectedInstance(2)

	moved := l.MoveDown()
	require.True(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "c", l.items[0].Title)
	require.Equal(t, "a", l.items[1].Title)
	require.Equal(t, "b", l.items[2].Title)
}

func TestMoveWithSingleItem(t *testing.T) {
	l := newTestListWithTitles("only")
	l.SetSelectedInstance(0)

	require.False(t, l.MoveUp())
	require.False(t, l.MoveDown())
}

func titlesOf(l *List) []string {
	out := make([]string, len(l.items))
	for i, inst := range l.items {
		out[i] = inst.Title
	}
	return out
}

func TestAddInstance_InsertsAtBottomOfActiveGroup(t *testing.T) {
	l := newTestListWithGroups([]string{"a", "p1", "p2"}, "p1", "p2")

	inst, _ := session.NewInstance(session.InstanceOptions{Title: "new", Path: ".", Program: "echo"})
	l.AddInstance(inst)

	require.Equal(t, []string{"a", "new", "p1", "p2"}, titlesOf(l))
}

func TestAddInstance_PausedGoesToEnd(t *testing.T) {
	l := newTestListWithGroups([]string{"a", "p1"}, "p1")

	inst, _ := session.NewInstance(session.InstanceOptions{Title: "p2", Path: ".", Program: "echo"})
	inst.Status = session.Paused
	l.AddInstance(inst)

	require.Equal(t, []string{"a", "p1", "p2"}, titlesOf(l))
}

func TestMoveToGroupBoundary_Resume(t *testing.T) {
	l := newTestListWithGroups([]string{"a", "b", "p1", "p2"}, "p1", "p2")

	// p2 was just resumed: it is no longer paused and should land below "b".
	resumed := l.items[3]
	resumed.Status = session.Ready
	l.MoveToGroupBoundary(resumed)

	require.Equal(t, []string{"a", "b", "p2", "p1"}, titlesOf(l))
	require.Equal(t, 2, l.selectedIdx)
}

func TestMoveToGroupBoundary_Pause(t *testing.T) {
	l := newTestListWithGroups([]string{"a", "b", "c", "p1"}, "p1")

	// "a" was just paused: it should land at the top of the paused group.
	paused := l.items[0]
	paused.Status = session.Paused
	l.MoveToGroupBoundary(paused)

	require.Equal(t, []string{"b", "c", "a", "p1"}, titlesOf(l))
	require.Equal(t, 2, l.selectedIdx)
}

func TestMove_StaysWithinGroup(t *testing.T) {
	l := newTestListWithGroups([]string{"a", "b", "p1", "p2"}, "p1", "p2")

	// Last active item wraps to the top of the active group, not into the paused group.
	l.SetSelectedInstance(1)
	require.True(t, l.MoveDown())
	require.Equal(t, []string{"b", "a", "p1", "p2"}, titlesOf(l))
	require.Equal(t, 0, l.selectedIdx)

	// First paused item wraps to the bottom of the paused group.
	l.SetSelectedInstance(2)
	require.True(t, l.MoveUp())
	require.Equal(t, []string{"b", "a", "p2", "p1"}, titlesOf(l))
	require.Equal(t, 3, l.selectedIdx)
}

func TestMove_SingleItemGroupIsNoop(t *testing.T) {
	l := newTestListWithGroups([]string{"a", "p1"}, "p1")

	l.SetSelectedInstance(0)
	require.False(t, l.MoveUp())
	require.False(t, l.MoveDown())
	l.SetSelectedInstance(1)
	require.False(t, l.MoveUp())
	require.False(t, l.MoveDown())
}

func TestGroupDivider_OnlyWhenBothGroupsPresent(t *testing.T) {
	l := newTestListWithGroups([]string{"a", "p1"}, "p1")
	l.SetSize(60, 40)
	require.Contains(t, l.String(), "paused")

	l = newTestListWithGroups([]string{"a", "b"})
	l.SetSize(60, 40)
	require.NotContains(t, l.String(), "paused")
}

func TestAddInstance_SelectInstanceFindsNewItem(t *testing.T) {
	// AddInstance no longer appends, so callers must locate the new instance by identity
	// rather than assuming it is last.
	l := newTestListWithGroups([]string{"a", "p1"}, "p1")

	inst, _ := session.NewInstance(session.InstanceOptions{Title: "new", Path: ".", Program: "echo"})
	l.AddInstance(inst)
	l.SelectInstance(inst)

	require.Same(t, inst, l.GetSelectedInstance())
}
