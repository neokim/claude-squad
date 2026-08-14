package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const readyIcon = "● "
const pausedIcon = "⏸ "

var readyStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var addedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var removedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

var pausedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})

var groupDividerStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#bbbbbb", Dark: "#555555"})

var titleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"})

var listDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})

var selectedTitleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var selectedDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var mainTitle = lipgloss.NewStyle().
	Background(lipgloss.Color("62")).
	Foreground(lipgloss.Color("230"))

var autoYesStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.Color("#1a1a1a"))

// matchHighlightStyle highlights search-matched characters in unselected
// titles by changing the background color (vim-style hlsearch).
var matchHighlightStyle = lipgloss.NewStyle().
	Bold(true).
	Background(lipgloss.Color("#ffd54f")).
	Foreground(lipgloss.Color("#1a1a1a"))

// matchHighlightSelectedStyle highlights search-matched characters within the
// selected row. The selected row already has a distinct background, so here
// we only change the foreground so the row's selection background stays intact.
var matchHighlightSelectedStyle = lipgloss.NewStyle().
	Bold(true).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.Color("#c44500"))

// selectedInnerStyle paints the selection background on each character of the
// selected title. We have to render character-by-character so that the
// highlighted match characters can interleave without losing their background;
// ANSI resets break lipgloss's outer-background inheritance, so every segment
// must declare the background itself.
var selectedInnerStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

type listRenderedItem struct {
	text string
	// divider marks the item the group divider is drawn above. The divider is only rendered
	// when the item is visible, so it costs nothing while scrolled out of view.
	divider bool
	lines   int
}

type List struct {
	items         []*session.Instance
	selectedIdx   int
	scrollOffset  int
	height, width int

	// freeScroll is set while the user scrolls the list directly (mouse wheel).
	// The viewport then stays put instead of following the selection. Any change
	// of the selection clears it, so moving the cursor scrolls it back into view.
	freeScroll      bool
	lastSelectedIdx int

	renderer      *InstanceRenderer
	autoyes       bool

	// searchQuery is the active search query (empty when no search is in progress).
	// Used so the renderer can highlight matching characters in instance titles.
	searchQuery string

	// map of repo name to number of instances using it. Used to display the repo name only if there are
	// multiple repos in play.
	repos map[string]int
}

func NewList(spinner *spinner.Model, autoYes bool) *List {
	return &List{
		items:    []*session.Instance{},
		renderer: &InstanceRenderer{spinner: spinner},
		repos:    make(map[string]int),
		autoyes:  autoYes,
	}
}

// SetSize sets the height and width of the list.
func (l *List) SetSize(width, height int) {
	l.width = width
	l.height = height
	l.renderer.setWidth(width)
}

// SetSessionPreviewSize sets the height and width for the tmux sessions. This makes the stdout line have the correct
// width and height.
func (l *List) SetSessionPreviewSize(width, height int) (err error) {
	for i, item := range l.items {
		if !item.Started() || item.Inactive() {
			continue
		}

		if innerErr := item.SetPreviewSize(width, height); innerErr != nil {
			err = errors.Join(
				err, fmt.Errorf("could not set preview size for instance %d: %v", i, innerErr))
		}
	}
	return
}

func (l *List) NumInstances() int {
	return len(l.items)
}

// InstanceRenderer handles rendering of session.Instance objects
type InstanceRenderer struct {
	spinner *spinner.Model
	width   int
}

func (r *InstanceRenderer) setWidth(width int) {
	r.width = AdjustPreviewWidth(width)
}

// ɹ and ɻ are other options.
const branchIcon = "Ꮧ"

func (r *InstanceRenderer) Render(i *session.Instance, idx int, selected bool, hasMultipleRepos bool, searchQuery string) string {
	prefix := fmt.Sprintf(" %d. ", idx)
	if idx >= 10 {
		prefix = prefix[:len(prefix)-1]
	}
	titleS := selectedTitleStyle
	descS := selectedDescStyle
	if !selected {
		titleS = titleStyle
		descS = listDescStyle
	}

	// add spinner next to title if it's running.
	// When the row is selected, every cell of the status icon must declare the
	// selection background explicitly — lipgloss doesn't fill an outer
	// background into inner ANSI-styled segments (icons set only foreground).
	selBg := lipgloss.Color("#dde4f0")
	readyS := readyStyle
	pausedS := pausedStyle
	if selected {
		readyS = readyS.Background(selBg)
		pausedS = pausedS.Background(selBg)
	}
	var join string
	switch i.Status {
	case session.Running, session.Loading, session.Pausing:
		spin := r.spinner.View()
		trailing := " "
		if selected {
			// The spinner is rendered with no foreground (terminal default),
			// which on a dark terminal is a light color — invisible on the
			// light selection background. Force a dark foreground here.
			spin = lipgloss.NewStyle().
				Background(selBg).
				Foreground(lipgloss.Color("#1a1a1a")).
				Render(spin)
			trailing = selectedInnerStyle.Render(trailing)
		}
		join = spin + trailing
	case session.Ready:
		join = readyS.Render(readyIcon)
	case session.Paused:
		join = pausedS.Render(pausedIcon)
	default:
	}

	// Cut the title if it's too long
	titleText := i.Title
	widthAvail := r.width - 3 - runewidth.StringWidth(prefix) - 1
	if widthAvail > 0 && runewidth.StringWidth(titleText) > widthAvail {
		titleText = runewidth.Truncate(titleText, widthAvail-3, "...")
	}
	titleInner := highlightTitle(prefix, titleText, searchQuery, selected)
	var placeOpts []lipgloss.WhitespaceOption
	sep := " "
	if selected {
		// When inner content contains ANSI-styled segments, lipgloss does NOT
		// auto-fill the outer style's background on the padding cells. We have
		// to declare it explicitly on the Place padding AND on the separator
		// between title and status icon, otherwise the row's selection
		// background breaks after the first styled segment.
		placeOpts = append(placeOpts, lipgloss.WithWhitespaceBackground(lipgloss.Color("#dde4f0")))
		sep = selectedInnerStyle.Render(" ")
	}
	title := titleS.Render(lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.Place(r.width-3, 1, lipgloss.Left, lipgloss.Center, titleInner, placeOpts...),
		sep,
		join,
	))

	stat := i.GetDiffStats()

	var diff string
	var addedDiff, removedDiff string
	if stat == nil || stat.Error != nil || stat.IsEmpty() {
		// Don't show diff stats if there's an error or if they don't exist
		addedDiff = ""
		removedDiff = ""
		diff = ""
	} else {
		addedDiff = fmt.Sprintf("+%d", stat.Added)
		removedDiff = fmt.Sprintf("-%d ", stat.Removed)
		diff = lipgloss.JoinHorizontal(
			lipgloss.Center,
			addedLinesStyle.Background(descS.GetBackground()).Render(addedDiff),
			lipgloss.Style{}.Background(descS.GetBackground()).Foreground(descS.GetForeground()).Render(","),
			removedLinesStyle.Background(descS.GetBackground()).Render(removedDiff),
		)
	}

	remainingWidth := r.width
	remainingWidth -= runewidth.StringWidth(prefix)
	remainingWidth -= runewidth.StringWidth(branchIcon)
	remainingWidth -= 2 // for the literal " " and "-" in the branchLine format string

	diffWidth := runewidth.StringWidth(addedDiff) + runewidth.StringWidth(removedDiff)
	if diffWidth > 0 {
		diffWidth += 1
	}

	// Use fixed width for diff stats to avoid layout issues
	remainingWidth -= diffWidth

	branch := i.Branch
	if i.Started() && hasMultipleRepos {
		repoName, err := i.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name in instance renderer: %v", err)
		} else {
			branch += fmt.Sprintf(" (%s)", repoName)
		}
	}
	// Don't show branch if there's no space for it. Or show ellipsis if it's too long.
	branchWidth := runewidth.StringWidth(branch)
	if remainingWidth < 0 {
		branch = ""
	} else if remainingWidth < branchWidth {
		if remainingWidth < 3 {
			branch = ""
		} else {
			// We know the remainingWidth is at least 4 and branch is longer than that, so this is safe.
			branch = runewidth.Truncate(branch, remainingWidth-3, "...")
		}
	}
	remainingWidth -= runewidth.StringWidth(branch)

	// Add spaces to fill the remaining width.
	spaces := ""
	if remainingWidth > 0 {
		spaces = strings.Repeat(" ", remainingWidth)
	}

	branchLine := fmt.Sprintf("%s %s-%s%s%s", strings.Repeat(" ", len(prefix)), branchIcon, branch, spaces, diff)

	// join title and subtitle
	text := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		descS.Render(branchLine),
	)

	return text
}

// renderGroupDivider renders the line separating the active group from the paused group.
func (l *List) renderGroupDivider() string {
	const label = " paused "
	width := AdjustPreviewWidth(l.width) + 2
	fill := width - runewidth.StringWidth(label)
	if fill < 2 {
		return groupDividerStyle.Render(label)
	}
	left := fill / 2
	return groupDividerStyle.Render(
		strings.Repeat("─", left) + label + strings.Repeat("─", fill-left))
}

func (l *List) String() string {
	titleText := " Instances "
	if total := len(l.items); total > 0 {
		titleText = fmt.Sprintf(" Instances %d/%d ", l.selectedIdx+1, total)
	}
	const autoYesText = " auto-yes "

	// Write the title.
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("\n")

	// Write title line
	// add padding of 2 because the border on list items adds some extra characters
	titleWidth := AdjustPreviewWidth(l.width) + 2
	if !l.autoyes {
		b.WriteString(lipgloss.Place(
			titleWidth, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText)))
	} else {
		title := lipgloss.Place(
			titleWidth/2, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText))
		autoYes := lipgloss.Place(
			titleWidth-(titleWidth/2), 1, lipgloss.Right, lipgloss.Bottom, autoYesStyle.Render(autoYesText))
		b.WriteString(lipgloss.JoinHorizontal(
			lipgloss.Top, title, autoYes))
	}

	b.WriteString("\n")
	b.WriteString("\n")

	// Header: 2 newlines + title line + 2 newlines = 4 lines consumed before items.
	headerLines := 4

	// Available lines for list items.
	availableLines := l.height - headerLines
	if availableLines < 1 {
		availableLines = 1
	}

	dividerIdx := l.dividerIndex()

	// Render all items and measure their line heights (without separator).
	rendered := make([]listRenderedItem, len(l.items))
	for i, item := range l.items {
		text := l.renderer.Render(item, i+1, i == l.selectedIdx, len(l.repos) > 1, l.searchQuery)
		lineCount := strings.Count(text, "\n") + 1
		rendered[i] = listRenderedItem{text: text, lines: lineCount}
		if i == dividerIdx {
			// The divider plus a blank line below it read as their own band above the item.
			rendered[i].divider = true
			rendered[i].lines += 2
		}
	}
	if len(rendered) == 0 {
		return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, b.String())
	}
	// A selection change cancels free scrolling so the viewport follows the cursor again.
	if l.selectedIdx != l.lastSelectedIdx {
		l.lastSelectedIdx = l.selectedIdx
		l.freeScroll = false
	}
	if l.scrollOffset >= len(rendered) {
		l.scrollOffset = len(rendered) - 1
	}
	// Adjust scroll offset to keep the selected item visible.
	if !l.freeScroll {
		l.adjustScrollOffset(rendered, availableLines)
	}

	// Render only items that fit within the viewport.
	// Note: "\n\n" between items adds 2 newline chars but only 1 visible line,
	// because the first \n terminates the previous item's last line.
	linesUsed := 0
	lastVisible := l.scrollOffset
	for i := l.scrollOffset; i < len(rendered); i++ {
		needed := rendered[i].lines
		if i > l.scrollOffset {
			needed += 1 // separator "\n\n" adds 1 empty line between items
		}
		if linesUsed+needed > availableLines && i > l.scrollOffset {
			break
		}
		lastVisible = i
		linesUsed += needed
	}
	for i := l.scrollOffset; i <= lastVisible; i++ {
		if rendered[i].divider {
			b.WriteString(l.renderGroupDivider())
			b.WriteString("\n\n")
		}
		b.WriteString(rendered[i].text)
		if i != lastVisible {
			b.WriteString("\n\n")
		}
	}

	return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, b.String())
}

// dividerIndex returns the index of the item the group divider is drawn above, or -1 when
// either group is empty and no divider is shown.
func (l *List) dividerIndex() int {
	boundary := groupBoundary(l.items)
	if boundary == 0 || boundary == len(l.items) {
		return -1
	}
	return boundary
}

// clampScrollOffset reduces the scroll offset so that the last item aligns
// with the bottom of the viewport (no trailing empty space).
func (l *List) clampScrollOffset() {
	if l.scrollOffset <= 0 || len(l.items) == 0 {
		return
	}
	// Each item is approximately 4 lines; separator adds 1 line between items.
	// Use the same headerLines constant as String().
	headerLines := 4
	availableLines := l.height - headerLines
	if availableLines < 1 {
		return
	}

	dividerIdx := l.dividerIndex()
	for l.scrollOffset > 0 {
		// Calculate total lines from scrollOffset to end.
		linesUsed := 0
		for i := l.scrollOffset; i < len(l.items); i++ {
			lines := 4 // approximate item height
			if i > l.scrollOffset {
				lines += 1 // separator
			}
			if i == dividerIdx {
				lines += 2 // group divider + its blank line
			}
			linesUsed += lines
		}
		if linesUsed >= availableLines {
			break
		}
		l.scrollOffset--
	}
}

// adjustScrollOffset ensures the selected item is visible within the viewport.
func (l *List) adjustScrollOffset(rendered []listRenderedItem, availableLines int) {
	if len(rendered) == 0 {
		return
	}

	// If selected is above the scroll offset, scroll up.
	if l.selectedIdx < l.scrollOffset {
		l.scrollOffset = l.selectedIdx
		return
	}

	// If selected is below the visible area, scroll down.
	linesUsed := 0
	for i := l.scrollOffset; i <= l.selectedIdx && i < len(rendered); i++ {
		needed := rendered[i].lines
		if i > l.scrollOffset {
			needed += 1 // separator adds 1 empty line
		}
		linesUsed += needed
	}
	for linesUsed > availableLines && l.scrollOffset < l.selectedIdx {
		linesUsed -= rendered[l.scrollOffset].lines + 1 // remove item + its trailing separator
		l.scrollOffset++
	}
}

// ScrollUp scrolls the viewport up by one item, leaving the selection alone.
func (l *List) ScrollUp() {
	if l.scrollOffset > 0 {
		l.scrollOffset--
		l.freeScroll = true
	}
}

// ScrollDown scrolls the viewport down by one item, leaving the selection alone.
// clampScrollOffset stops it once the last item reaches the bottom of the viewport.
func (l *List) ScrollDown() {
	if l.scrollOffset >= len(l.items)-1 {
		return
	}
	l.scrollOffset++
	l.clampScrollOffset()
	l.freeScroll = true
}

// Down selects the next item in the list. Wraps to top when at the bottom.
func (l *List) Down() {
	if len(l.items) == 0 {
		return
	}
	if l.selectedIdx < len(l.items)-1 {
		l.selectedIdx++
	} else {
		l.selectedIdx = 0
		l.scrollOffset = 0
	}
}

// Kill selects the next item in the list.
func (l *List) Kill() {
	if len(l.items) == 0 {
		return
	}
	targetInstance := l.items[l.selectedIdx]

	// Kill the tmux session
	if err := targetInstance.Kill(); err != nil {
		log.ErrorLog.Printf("could not kill instance: %v", err)
	}

	// If you delete the last one in the list, select the previous one.
	if l.selectedIdx == len(l.items)-1 {
		defer l.Up()
	}

	// Unregister the reponame.
	repoName, err := targetInstance.RepoName()
	if err != nil {
		log.ErrorLog.Printf("could not get repo name: %v", err)
	} else {
		l.rmRepo(repoName)
	}

	// Since there's items after this, the selectedIdx can stay the same.
	l.items = append(l.items[:l.selectedIdx], l.items[l.selectedIdx+1:]...)

	// Adjust scroll offset so there's no empty space at the bottom after deletion.
	if l.scrollOffset > 0 && l.scrollOffset >= len(l.items) {
		l.scrollOffset = len(l.items) - 1
	}
	l.clampScrollOffset()
}

func (l *List) Attach() (chan struct{}, error) {
	targetInstance := l.items[l.selectedIdx]
	return targetInstance.Attach()
}

// Up selects the prev item in the list. Wraps to bottom when at the top.
func (l *List) Up() {
	if len(l.items) == 0 {
		return
	}
	if l.selectedIdx > 0 {
		l.selectedIdx--
	} else {
		l.selectedIdx = len(l.items) - 1
	}
}

func (l *List) addRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		l.repos[repo] = 0
	}
	l.repos[repo]++
}

func (l *List) rmRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		log.ErrorLog.Printf("repo %s not found", repo)
		return
	}
	l.repos[repo]--
	if l.repos[repo] == 0 {
		delete(l.repos, repo)
	}
}

// AddInstance adds a new instance to the list. Active instances go to the bottom of the active
// group; paused ones are appended at the end so that a batch restore from storage keeps its
// relative order.
// It returns a finalizer function that should be called when the instance
// is started. If the instance was restored from storage or is paused, you can call the finalizer immediately.
// When creating a new one and entering the name, you want to call the finalizer once the name is done.
func (l *List) AddInstance(instance *session.Instance) (finalize func()) {
	idx := len(l.items)
	if !instance.Inactive() {
		idx = groupBoundary(l.items)
	}
	l.insertAt(idx, instance)
	// The finalizer registers the repo name once the instance is started.
	return func() {
		repoName, err := instance.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name: %v", err)
			return
		}

		l.addRepo(repoName)
	}
}

// GetSelectedInstance returns the currently selected instance
func (l *List) GetSelectedInstance() *session.Instance {
	if len(l.items) == 0 {
		return nil
	}
	return l.items[l.selectedIdx]
}

// SetSelectedInstance sets the selected index. Noop if the index is out of bounds.
func (l *List) SetSelectedInstance(idx int) {
	if idx >= len(l.items) {
		return
	}
	l.selectedIdx = idx
}

// SelectInstance finds and selects the given instance in the list.
func (l *List) SelectInstance(target *session.Instance) {
	for i, inst := range l.items {
		if inst == target {
			l.SetSelectedInstance(i)
			return
		}
	}
}

// MoveUp swaps the selected instance with the one above it. Wraps to the
// bottom when at the top.
func (l *List) MoveUp() bool {
	lo, hi, ok := l.selectedGroupRange()
	if !ok || hi == lo {
		return false
	}
	if l.selectedIdx == lo {
		first := l.items[lo]
		copy(l.items[lo:hi], l.items[lo+1:hi+1])
		l.items[hi] = first
		l.selectedIdx = hi
		return true
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx-1] = l.items[l.selectedIdx-1], l.items[l.selectedIdx]
	l.selectedIdx--
	return true
}

// MoveDown swaps the selected instance with the one below it. Wraps to the
// top when at the bottom.
func (l *List) MoveDown() bool {
	lo, hi, ok := l.selectedGroupRange()
	if !ok || hi == lo {
		return false
	}
	if l.selectedIdx == hi {
		last := l.items[hi]
		copy(l.items[lo+1:hi+1], l.items[lo:hi])
		l.items[lo] = last
		l.selectedIdx = lo
		return true
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx+1] = l.items[l.selectedIdx+1], l.items[l.selectedIdx]
	l.selectedIdx++
	return true
}

// groupBoundary returns the index of the first paused instance, i.e. the frontier between the
// active group (top) and the paused group (bottom).
func groupBoundary(items []*session.Instance) int {
	for i, inst := range items {
		if inst.Inactive() {
			return i
		}
	}
	return len(items)
}

// selectedGroupRange returns the inclusive [lo, hi] index range of the group the selected
// instance belongs to. ok is false when there is no selection.
func (l *List) selectedGroupRange() (lo, hi int, ok bool) {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return 0, 0, false
	}
	boundary := groupBoundary(l.items)
	if l.selectedIdx < boundary {
		return 0, boundary - 1, true
	}
	return boundary, len(l.items) - 1, true
}

// insertAt inserts instance at idx, shifting the rest down.
func (l *List) insertAt(idx int, instance *session.Instance) {
	l.items = append(l.items, nil)
	copy(l.items[idx+1:], l.items[idx:])
	l.items[idx] = instance
}

// MoveToGroupBoundary moves an instance whose paused state just changed to the frontier between
// the active and paused groups, and puts the cursor on it. A resumed instance lands at the bottom
// of the active group and a paused one at the top of the paused group -- the same index either way.
func (l *List) MoveToGroupBoundary(target *session.Instance) {
	idx := slices.Index(l.items, target)
	if idx < 0 {
		return
	}
	l.items = append(l.items[:idx], l.items[idx+1:]...)
	boundary := groupBoundary(l.items)
	l.insertAt(boundary, target)
	l.SetSelectedInstance(boundary)
	l.clampScrollOffset()
}

// GetInstances returns all instances in the list
func (l *List) GetInstances() []*session.Instance {
	return l.items
}

// GetSelectedIdx returns the currently selected index.
func (l *List) GetSelectedIdx() int {
	return l.selectedIdx
}

// Search returns the indices of instances whose Title fuzzy-matches the query.
// Matching is case-insensitive and uses subsequence matching, so "aut" matches
// "authentication". An empty query returns no matches.
func (l *List) Search(query string) []int {
	if query == "" {
		return nil
	}
	q := []rune(strings.ToLower(query))
	var matches []int
	for i, item := range l.items {
		if subsequenceMatchPositions(q, []rune(strings.ToLower(item.Title))) != nil {
			matches = append(matches, i)
		}
	}
	return matches
}

// SetSearchQuery records the active search query so the renderer can
// highlight matched characters in instance titles. An empty string disables
// highlighting.
func (l *List) SetSearchQuery(q string) {
	l.searchQuery = q
}

// highlightTitle renders "<prefix> <titleText>" with matching characters of
// titleText highlighted according to searchQuery. If searchQuery is empty or
// doesn't match (e.g. truncated title lost the match), the plain string is
// returned. Matching is re-computed against the (possibly truncated) titleText
// so highlighting stays correct after ellipsizing.
//
// For the selected row, every character (including non-matching ones and the
// prefix) is wrapped in selectedInnerStyle so that the selection background
// stays continuous — ANSI reset after a match segment otherwise blanks the
// background of the following plain characters.
func highlightTitle(prefix, titleText, searchQuery string, selected bool) string {
	plain := prefix + " " + titleText
	if searchQuery == "" {
		return plain
	}
	q := []rune(strings.ToLower(searchQuery))
	t := []rune(strings.ToLower(titleText))
	positions := subsequenceMatchPositions(q, t)
	if len(positions) == 0 {
		return plain
	}
	matchStyle := matchHighlightStyle
	if selected {
		matchStyle = matchHighlightSelectedStyle
	}
	matchSet := make(map[int]bool, len(positions))
	for _, idx := range positions {
		matchSet[idx] = true
	}
	runes := []rune(titleText)
	var sb strings.Builder
	if selected {
		sb.WriteString(selectedInnerStyle.Render(prefix + " "))
	} else {
		sb.WriteString(prefix)
		sb.WriteString(" ")
	}
	for i, ch := range runes {
		switch {
		case matchSet[i]:
			sb.WriteString(matchStyle.Render(string(ch)))
		case selected:
			sb.WriteString(selectedInnerStyle.Render(string(ch)))
		default:
			sb.WriteString(string(ch))
		}
	}
	return sb.String()
}

// subsequenceMatchPositions returns the union of rune positions in target at
// which every whitespace-separated token of query matched as a subsequence
// (each token matched independently against the full target, fzf-style AND).
// Returns nil if any token fails to match. Both inputs must be lower-cased.
func subsequenceMatchPositions(query, target []rune) []int {
	if len(query) == 0 {
		return nil
	}
	tokens := splitTokens(query)
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[int]struct{})
	for _, tok := range tokens {
		pos := matchTokenPositions(tok, target)
		if pos == nil {
			return nil
		}
		for _, p := range pos {
			seen[p] = struct{}{}
		}
	}
	positions := make([]int, 0, len(seen))
	for p := range seen {
		positions = append(positions, p)
	}
	sort.Ints(positions)
	return positions
}

// matchTokenPositions returns the indices in target where token's runes appear
// in order (subsequence). Returns nil if any rune is missing.
func matchTokenPositions(token, target []rune) []int {
	if len(token) == 0 {
		return nil
	}
	positions := make([]int, 0, len(token))
	j := 0
	for i := 0; i < len(target) && j < len(token); i++ {
		if target[i] == token[j] {
			positions = append(positions, i)
			j++
		}
	}
	if j != len(token) {
		return nil
	}
	return positions
}

// splitTokens splits query on whitespace runs, dropping empties.
func splitTokens(query []rune) [][]rune {
	var tokens [][]rune
	start := -1
	for i, r := range query {
		if r == ' ' {
			if start >= 0 {
				tokens = append(tokens, query[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		tokens = append(tokens, query[start:])
	}
	return tokens
}
