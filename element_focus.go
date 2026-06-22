package tui

import (
	"github.com/grindlemire/go-tui/internal/debug"
)

// --- Focus API ---

// IsFocusable returns whether this element can receive focus.
func (e *Element) IsFocusable() bool {
	return e.focusable
}

// IsTabStop returns whether this element participates in Tab/Shift+Tab navigation.
func (e *Element) IsTabStop() bool {
	return e.tabStop
}

// IsAutoFocus returns whether this element should receive focus automatically
// when the element tree is first applied.
func (e *Element) IsAutoFocus() bool {
	return e.autoFocus
}

// IsFocused returns whether this element currently has focus.
func (e *Element) IsFocused() bool {
	return e.focused
}

// Focus marks this element as focused and calls onFocus callback if set.
// Idempotent: no-op if already focused.
// Does not cascade to children — only the FocusManager target receives focus.
//
// For elements with a border and no explicit onFocus handler, a default
// cyan border highlight is applied automatically.
func (e *Element) Focus() {
	if e.focused {
		return
	}
	e.focused = true
	if e.onFocus != nil {
		e.onFocus(e)
	} else if e.border != BorderNone {
		e.savedBorderStyle = e.borderStyle
		e.hasSavedBorder = true
		e.borderStyle = NewStyle().Foreground(Cyan)
		e.MarkDirty()
	}
}

// Blur marks this element as not focused and calls onBlur callback if set.
// Idempotent: no-op if already blurred.
// Does not cascade to children — only the FocusManager target loses focus.
//
// Restores the original border style if a default focus highlight was applied.
func (e *Element) Blur() {
	if !e.focused {
		return
	}
	e.focused = false
	if e.onBlur != nil {
		e.onBlur(e)
	} else if e.hasSavedBorder {
		e.borderStyle = e.savedBorderStyle
		e.hasSavedBorder = false
		e.MarkDirty()
	}
}

// Activate triggers the onActivate callback if set.
// Returns true if the callback existed and was called.
func (e *Element) Activate() bool {
	if e.onActivate != nil {
		e.onActivate()
		return true
	}
	return false
}

// SetFocusable sets whether this element can receive focus.
// Also sets tabStop to the same value.
func (e *Element) SetFocusable(focusable bool) {
	e.focusable = focusable
	e.tabStop = focusable
}

// SetOnFocus sets a handler that's called when this element gains focus.
// The handler receives the element as its first parameter (self-inject).
// Implicitly sets focusable = true and tabStop = true.
func (e *Element) SetOnFocus(fn func(*Element)) {
	e.focusable = true
	e.tabStop = true
	e.onFocus = fn
}

// SetOnBlur sets a handler that's called when this element loses focus.
// The handler receives the element as its first parameter (self-inject).
// Implicitly sets focusable = true and tabStop = true.
func (e *Element) SetOnBlur(fn func(*Element)) {
	e.focusable = true
	e.tabStop = true
	e.onBlur = fn
}

// HandleEvent dispatches an event to this element's handler.
// Only handles scroll events for scrollable elements.
// If the event is a scroll event and this element doesn't consume it,
// propagates up the tree to the nearest scrollable ancestor.
// Returns true if the event was consumed.
func (e *Element) HandleEvent(event Event) bool {
	debug.Log("Element.HandleEvent: event=%T text=%q scrollMode=%v", event, e.text, e.scrollMode)

	// Handle scroll events for scrollable elements
	if e.scrollMode != ScrollNone {
		if e.handleScrollEvent(event) {
			return true
		}
	}

	// Propagate scroll events (mouse wheel, scroll keys) up to
	// the nearest scrollable ancestor. Without this, a non-scrollable
	// child (e.g. a button inside a scrollable container) absorbs the
	// event and the container never scrolls.
	if isScrollEvent(event) {
		for p := e.parent; p != nil; p = p.parent {
			if p.scrollMode != ScrollNone {
				if p.handleScrollEvent(event) {
					return true
				}
				break // first scrollable ancestor tried and declined
			}
		}
	}

	debug.Log("Element.HandleEvent: event not consumed")
	return false
}

// isScrollEvent returns true if the event is a mouse wheel or scroll key event.
func isScrollEvent(event Event) bool {
	if mouse, ok := event.(MouseEvent); ok {
		return mouse.Button == MouseWheelUp || mouse.Button == MouseWheelDown
	}
	if key, ok := event.(KeyEvent); ok {
		switch key.Key {
		case KeyUp, KeyDown, KeyLeft, KeyRight, KeyPageUp, KeyPageDown, KeyHome, KeyEnd:
			return true
		}
	}
	return false
}

// ContainsPoint returns true if the screen-space point (x, y) is within the
// element's bounds. The element's layout.Rect is in content-space (relative to
// the content area origin of the nearest scrollable ancestor). To convert
// screen-space to content-space, we subtract each ancestor's screen position.
// For scrollable ancestors, children are positioned from (0,0) in content-space,
// so we add the scroll offset (content shifts up = more screen Y needed).
// ContainsPoint returns true if the screen-space point (x, y) is within the
// element's bounds. For elements inside scrollable containers, the element's
// layout.Rect is in content-space (relative to the content area origin of the
// scrollable ancestor, at (0,0) after padding+border). To convert screen-space
// to content-space, we accumulate the offset from scrollable ancestors only.
func (e *Element) ContainsPoint(x, y int) bool {
	cx, cy := x, y
	for p := e.parent; p != nil; p = p.parent {
		if p.scrollMode != ScrollNone {
			// Children are positioned relative to (0,0) in content-space,
			// which starts at border + padding inside the scrollable.
			// Subtract scrollable's screen position, then add scroll offset
			// and border+padding to get content-space coords.
			sx := p.Rect().X
			sy := p.Rect().Y
			if p.border != BorderNone {
				sx++
				sy++
			}
			sx += p.style.Padding.Left
			sy += p.style.Padding.Top
			cx = x - sx + p.scrollX
			cy = y - sy + p.scrollY
			break // only the nearest scrollable ancestor matters
		}
	}
	return e.layout.Rect.Contains(cx, cy)
}

// ContainsContentPoint returns true if the point (x, y) is within the
// element's content area (excluding border and padding). Uses the same
// scroll-aware coordinate conversion as ContainsPoint.
func (e *Element) ContainsContentPoint(x, y int) bool {
	cx, cy := x, y
	for p := e.parent; p != nil; p = p.parent {
		if p.scrollMode != ScrollNone {
			sx := p.Rect().X
			sy := p.Rect().Y
			if p.border != BorderNone {
				sx++
				sy++
			}
			sx += p.style.Padding.Left
			sy += p.style.Padding.Top
			cx = x - sx + p.scrollX
			cy = y - sy + p.scrollY
			break
		}
	}
	// Use ContentRect instead of Rect to exclude border and padding
	return e.layout.ContentRect.Contains(cx, cy)
}

// hasWrapOverflow returns true if this element has wrapped text that overflows
// its content area, enabling auto-scroll with no visible scrollbar.
func (e *Element) hasWrapOverflow() bool {
	if e.scrollMode != ScrollNone || e.text == "" || e.noWrap {
		return false
	}
	return e.contentHeight > 0 && e.contentHeight > e.ContentRect().Height
}

// scrollWrapOverflow adjusts scrollY for wrap-overflow elements.
// This bypasses the normal ScrollTo/ScrollBy which require scrollMode.
func (e *Element) scrollWrapOverflow(dy int) {
	cr := e.ContentRect()
	maxY := max(e.contentHeight-cr.Height, 0)
	newY := min(max(e.scrollY+dy, 0), maxY)
	if newY != e.scrollY {
		e.scrollY = newY
		e.MarkDirty()
	}
}

// handleScrollEvent handles keyboard and mouse wheel events for scrolling.
func (e *Element) handleScrollEvent(event Event) bool {
	// Handle mouse wheel events
	if mouse, ok := event.(MouseEvent); ok {
		switch mouse.Button {
		case MouseWheelUp:
			if e.scrollMode == ScrollVertical || e.scrollMode == ScrollBoth {
				e.ScrollBy(0, -1)
				return true
			}
			// Auto-scroll for wrap-overflow text
			if e.hasWrapOverflow() {
				e.scrollWrapOverflow(-1)
				return true
			}
		case MouseWheelDown:
			if e.scrollMode == ScrollVertical || e.scrollMode == ScrollBoth {
				e.ScrollBy(0, 1)
				return true
			}
			// Auto-scroll for wrap-overflow text
			if e.hasWrapOverflow() {
				e.scrollWrapOverflow(1)
				return true
			}
		}
		return false
	}

	key, ok := event.(KeyEvent)
	if !ok {
		return false
	}

	_, viewportHeight := e.ViewportSize()

	switch key.Key {
	case KeyUp:
		if e.scrollMode == ScrollVertical || e.scrollMode == ScrollBoth {
			e.ScrollBy(0, -1)
			return true
		}
	case KeyDown:
		if e.scrollMode == ScrollVertical || e.scrollMode == ScrollBoth {
			e.ScrollBy(0, 1)
			return true
		}
	case KeyLeft:
		if e.scrollMode == ScrollHorizontal || e.scrollMode == ScrollBoth {
			e.ScrollBy(-1, 0)
			return true
		}
	case KeyRight:
		if e.scrollMode == ScrollHorizontal || e.scrollMode == ScrollBoth {
			e.ScrollBy(1, 0)
			return true
		}
	case KeyPageUp:
		if e.scrollMode == ScrollVertical || e.scrollMode == ScrollBoth {
			e.ScrollBy(0, -viewportHeight)
			return true
		}
	case KeyPageDown:
		if e.scrollMode == ScrollVertical || e.scrollMode == ScrollBoth {
			e.ScrollBy(0, viewportHeight)
			return true
		}
	case KeyHome:
		e.ScrollTo(0, 0)
		return true
	case KeyEnd:
		e.ScrollToBottom()
		return true
	}

	return false
}
