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
// the content area of its ancestors). To convert screen-space to content-space,
// we subtract each ancestor's screen position and add back any scroll offsets.
func (e *Element) ContainsPoint(x, y int) bool {
	cx, cy := x, y
	for p := e.parent; p != nil; p = p.parent {
		// Subtract parent's screen position (Rect is absolute screen coords)
		cx -= p.Rect().X
		cy -= p.Rect().Y
		// For scrollable ancestors, children are positioned in content-space
		// starting from (0,0) within the content area. The content area starts
		// at (padding, padding) inside the parent. Scroll offset shifts
		// content up, so we add scroll offset to match screen coords.
		if p.scrollMode != ScrollNone {
			cx += p.scrollX
			cy += p.scrollY
			// Children are positioned relative to content area origin,
			// not the parent's padding inset. Add back the padding so
			// screen-to-content conversion is correct.
			cx += p.style.Padding.Left
			cy += p.style.Padding.Top
		}
	}
	return e.layout.Rect.Contains(cx, cy)
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
