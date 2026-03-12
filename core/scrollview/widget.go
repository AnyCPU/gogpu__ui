package scrollview

import (
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
)

// Widget implements a scrollable container that clips and translates its
// content child widget.
//
// A scroll view is created with [New] using functional options:
//
//	sv := scrollview.New(content,
//	    scrollview.DirectionOpt(scrollview.Vertical),
//	    scrollview.OnScroll(handleScroll),
//	)
type Widget struct {
	widget.WidgetBase
	cfg     config
	content widget.Widget
	painter Painter

	// Cached layout measurements.
	contentSize  geometry.Size
	viewportSize geometry.Size

	// Interaction state.
	hovered         bool
	dragging        dragAxis
	dragStart       geometry.Point
	dragScrollStart float32
}

// New creates a new scroll view Widget wrapping the given content widget.
//
// The returned widget is visible, enabled, and focusable by default.
// The default direction is [Vertical] with [ScrollbarAuto] visibility.
func New(content widget.Widget, opts ...Option) *Widget {
	w := &Widget{
		content: content,
		painter: DefaultPainter{},
	}
	w.SetVisible(true)
	w.SetEnabled(true)

	for _, opt := range opts {
		opt(&w.cfg)
	}

	if w.cfg.painter != nil {
		w.painter = w.cfg.painter
	}

	return w
}

// IsFocusable reports whether the scroll view can currently receive focus.
func (w *Widget) IsFocusable() bool {
	return w.IsVisible() && w.IsEnabled()
}

// Layout calculates the scroll view's size and measures its content.
//
// The viewport is constrained to the parent's constraints. The content
// is measured with unconstrained dimensions along the scroll axis to
// determine its natural size.
func (w *Widget) Layout(ctx widget.Context, constraints geometry.Constraints) geometry.Size {
	// The viewport fills the available space.
	w.viewportSize = constraints.Biggest()
	if w.viewportSize.Width <= 0 || w.viewportSize.Height <= 0 {
		w.viewportSize = constraints.Constrain(geometry.Sz(defaultViewportWidth, defaultViewportHeight))
	}

	if w.content == nil {
		return w.viewportSize
	}

	// Build content constraints: unconstrained along scroll axes.
	contentConstraints := w.buildContentConstraints()

	// Measure content.
	w.contentSize = w.content.Layout(ctx, contentConstraints)

	// Set content bounds at (0, 0) with its natural size.
	if setter, ok := w.content.(interface{ SetBounds(geometry.Rect) }); ok {
		setter.SetBounds(geometry.NewRect(0, 0, w.contentSize.Width, w.contentSize.Height))
	}

	return w.viewportSize
}

// buildContentConstraints creates constraints for measuring the content widget.
// Axes that scroll are unconstrained; non-scrolling axes are constrained to viewport.
func (w *Widget) buildContentConstraints() geometry.Constraints {
	switch w.cfg.direction {
	case Vertical:
		return geometry.Constraints{
			MinWidth:  w.viewportSize.Width,
			MaxWidth:  w.viewportSize.Width,
			MinHeight: 0,
			MaxHeight: geometry.Infinity,
		}
	case Horizontal:
		return geometry.Constraints{
			MinWidth:  0,
			MaxWidth:  geometry.Infinity,
			MinHeight: w.viewportSize.Height,
			MaxHeight: w.viewportSize.Height,
		}
	case Both:
		return geometry.Constraints{
			MinWidth:  0,
			MaxWidth:  geometry.Infinity,
			MinHeight: 0,
			MaxHeight: geometry.Infinity,
		}
	default:
		return geometry.Constraints{
			MinWidth:  w.viewportSize.Width,
			MaxWidth:  w.viewportSize.Width,
			MinHeight: 0,
			MaxHeight: geometry.Infinity,
		}
	}
}

// Default viewport dimensions used as fallback.
const (
	defaultViewportWidth  float32 = 200
	defaultViewportHeight float32 = 200
)

// Draw renders the scroll view to the canvas.
//
// Drawing order:
//  1. Push clip to viewport bounds
//  2. Push transform for scroll offset
//  3. Draw content
//  4. Pop transform
//  5. Pop clip
//  6. Draw scrollbar(s) on top
func (w *Widget) Draw(ctx widget.Context, canvas widget.Canvas) {
	bounds := w.Bounds()
	if bounds.IsEmpty() {
		return
	}

	scrollX := w.cfg.ResolvedScrollX()
	scrollY := w.cfg.ResolvedScrollY()

	// Clip content to viewport.
	canvas.PushClip(bounds)
	canvas.PushTransform(geometry.Pt(bounds.Min.X-scrollX, bounds.Min.Y-scrollY))

	if w.content != nil {
		w.content.Draw(ctx, canvas)
	}

	canvas.PopTransform()
	canvas.PopClip()

	// Draw scrollbar(s) on top.
	w.paintScrollbars(canvas)
}

// paintScrollbars renders scrollbar overlays.
func (w *Widget) paintScrollbars(canvas widget.Canvas) {
	vThumb, hThumb := w.computeThumbRects()
	vTrack, hTrack := w.computeTrackRects()

	ps := PaintState{
		Bounds:    w.Bounds(),
		Direction: w.cfg.direction,
		Focused:   w.IsFocused(),
		Hovered:   w.hovered,
		Dragging:  w.dragging != dragNone,

		VScrollVisible: w.shouldShowVScrollbar(),
		VThumbRect:     vThumb,
		VTrackRect:     vTrack,

		HScrollVisible: w.shouldShowHScrollbar(),
		HThumbRect:     hThumb,
		HTrackRect:     hTrack,
	}

	w.painter.PaintScrollbar(canvas, ps)
}

// Event handles an input event and returns true if consumed.
func (w *Widget) Event(ctx widget.Context, e event.Event) bool {
	// First try to pass events to the content child.
	if w.content != nil {
		if consumed := w.content.Event(ctx, e); consumed {
			return true
		}
	}

	return handleEvent(w, ctx, e)
}

// Children returns the content widget as the single child.
func (w *Widget) Children() []widget.Widget {
	if w.content == nil {
		return nil
	}
	return []widget.Widget{w.content}
}

// Mount creates signal bindings for push-based invalidation.
// Implements [widget.Lifecycle].
func (w *Widget) Mount(ctx widget.Context) {
	sched := ctx.Scheduler()
	if sched == nil {
		return
	}
	if w.cfg.readonlyScrollXSignal != nil {
		b := state.BindToScheduler(w.cfg.readonlyScrollXSignal, w, sched)
		w.AddBinding(b)
	} else if w.cfg.scrollXSignal != nil {
		b := state.BindToScheduler(w.cfg.scrollXSignal, w, sched)
		w.AddBinding(b)
	}
	if w.cfg.readonlyScrollYSignal != nil {
		b := state.BindToScheduler(w.cfg.readonlyScrollYSignal, w, sched)
		w.AddBinding(b)
	} else if w.cfg.scrollYSignal != nil {
		b := state.BindToScheduler(w.cfg.scrollYSignal, w, sched)
		w.AddBinding(b)
	}
}

// Unmount is called when the scroll view is removed from the widget tree.
// Implements [widget.Lifecycle].
func (w *Widget) Unmount() {
	// Bindings are cleaned up automatically by WidgetBase.CleanupBindings().
}

// Content returns the scroll view's content widget.
func (w *Widget) Content() widget.Widget {
	return w.content
}

// ScrollOffset returns the current scroll offset.
func (w *Widget) ScrollOffset() (x, y float32) {
	return w.cfg.ResolvedScrollX(), w.cfg.ResolvedScrollY()
}

// ViewportSize returns the current viewport size.
func (w *Widget) ViewportSize() geometry.Size {
	return w.viewportSize
}

// ContentSize returns the measured content size.
func (w *Widget) ContentSize() geometry.Size {
	return w.contentSize
}

// canScrollX reports whether horizontal scrolling is possible.
func (w *Widget) canScrollX() bool {
	if w.cfg.direction == Vertical {
		return false
	}
	return w.contentSize.Width > w.viewportSize.Width
}

// canScrollY reports whether vertical scrolling is possible.
func (w *Widget) canScrollY() bool {
	if w.cfg.direction == Horizontal {
		return false
	}
	return w.contentSize.Height > w.viewportSize.Height
}

// shouldShowVScrollbar returns true if the vertical scrollbar should be drawn.
func (w *Widget) shouldShowVScrollbar() bool {
	if w.cfg.direction == Horizontal {
		return false
	}
	switch w.cfg.scrollbar {
	case ScrollbarAlways:
		return true
	case ScrollbarNever:
		return false
	default: // ScrollbarAuto
		return w.contentSize.Height > w.viewportSize.Height
	}
}

// shouldShowHScrollbar returns true if the horizontal scrollbar should be drawn.
func (w *Widget) shouldShowHScrollbar() bool {
	if w.cfg.direction == Vertical {
		return false
	}
	switch w.cfg.scrollbar {
	case ScrollbarAlways:
		return true
	case ScrollbarNever:
		return false
	default: // ScrollbarAuto
		return w.contentSize.Width > w.viewportSize.Width
	}
}

// computeTrackRects calculates the track rectangles for both scrollbars.
func (w *Widget) computeTrackRects() (vTrack, hTrack geometry.Rect) {
	bounds := w.Bounds()
	showV := w.shouldShowVScrollbar()
	showH := w.shouldShowHScrollbar()

	if showV {
		vTrack = computeScrollbarRect(bounds, dragVertical, showH)
	}
	if showH {
		hTrack = computeScrollbarRect(bounds, dragHorizontal, showV)
	}
	return vTrack, hTrack
}

// computeThumbRects calculates the thumb rectangles for both scrollbars.
func (w *Widget) computeThumbRects() (vThumb, hThumb geometry.Rect) {
	vTrack, hTrack := w.computeTrackRects()

	if w.shouldShowVScrollbar() && !vTrack.IsEmpty() {
		vThumb = w.computeVThumbRect(vTrack)
	}
	if w.shouldShowHScrollbar() && !hTrack.IsEmpty() {
		hThumb = w.computeHThumbRect(hTrack)
	}
	return vThumb, hThumb
}

// computeVThumbRect calculates the vertical thumb rectangle within the track.
func (w *Widget) computeVThumbRect(track geometry.Rect) geometry.Rect {
	trackLen := track.Height() - scrollbarPadding*2
	if trackLen <= 0 {
		return geometry.Rect{}
	}

	thumbSize := computeThumbSize(w.viewportSize.Height, w.contentSize.Height, trackLen)
	maxScroll := w.contentSize.Height - w.viewportSize.Height
	thumbPos := computeThumbPosition(w.cfg.ResolvedScrollY(), maxScroll, trackLen, thumbSize)

	return geometry.NewRect(
		track.Min.X+scrollbarPadding,
		track.Min.Y+scrollbarPadding+thumbPos,
		scrollbarWidth,
		thumbSize,
	)
}

// computeHThumbRect calculates the horizontal thumb rectangle within the track.
func (w *Widget) computeHThumbRect(track geometry.Rect) geometry.Rect {
	trackLen := track.Width() - scrollbarPadding*2
	if trackLen <= 0 {
		return geometry.Rect{}
	}

	thumbSize := computeThumbSize(w.viewportSize.Width, w.contentSize.Width, trackLen)
	maxScroll := w.contentSize.Width - w.viewportSize.Width
	thumbPos := computeThumbPosition(w.cfg.ResolvedScrollX(), maxScroll, trackLen, thumbSize)

	return geometry.NewRect(
		track.Min.X+scrollbarPadding+thumbPos,
		track.Min.Y+scrollbarPadding,
		thumbSize,
		scrollbarWidth,
	)
}

// Padding sets the content padding. Returns the widget for method chaining.
func (w *Widget) Padding(_ float32) *Widget {
	// Reserved for future use. Currently a no-op.
	return w
}

// Verify Widget implements required interfaces at compile time.
var (
	_ widget.Widget    = (*Widget)(nil)
	_ widget.Focusable = (*Widget)(nil)
	_ widget.Lifecycle = (*Widget)(nil)
)
