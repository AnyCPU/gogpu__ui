package primitives

import (
	"image"
	"image/draw"

	"github.com/gogpu/gg"
	"github.com/gogpu/ui/a11y"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	internalRender "github.com/gogpu/ui/internal/render"
	"github.com/gogpu/ui/widget"
)

// RepaintBoundary is a display widget that caches its child subtree as a
// CPU-side pixel buffer (image.RGBA). When the child subtree is clean (no
// dirty widgets), the cached image is composited directly onto the parent
// canvas instead of re-executing Draw on every descendant.
//
// This is the Flutter RepaintBoundary pattern: an explicit opt-in boundary
// that isolates expensive subtrees from the rest of the render tree.
// Users wrap widgets in RepaintBoundary at points where subtrees are
// expensive to draw and rarely change.
//
// Cache lifecycle:
//   - The cache is allocated on first draw (lazy).
//   - The cache is invalidated when any descendant is dirty or the size changes.
//   - The cache is freed on Unmount or when the widget is garbage collected.
//
// RepaintBoundary implements [widget.Widget] and [a11y.Accessible].
//
// Example:
//
//	expensive := primitives.Box(
//	    primitives.Text("Complex chart..."),
//	).Padding(16)
//
//	cached := primitives.NewRepaintBoundary(expensive)
type RepaintBoundary struct {
	widget.WidgetBase

	child widget.Widget

	// cache holds the rendered child subtree as an RGBA pixmap.
	cache *image.RGBA
	// cacheValid indicates whether the cache is up to date.
	cacheValid bool
	// cacheWidth and cacheHeight track the cache dimensions to detect
	// size changes that require reallocation.
	cacheWidth  int
	cacheHeight int

	// debugLabel is an optional identifier for diagnostics.
	debugLabel string

	// cacheHits tracks how many times the cache was used (for stats).
	cacheHits int
}

// Option configures a [RepaintBoundary].
type Option func(*RepaintBoundary)

// WithDebugLabel sets an optional label for diagnostics and logging.
func WithDebugLabel(label string) Option {
	return func(rb *RepaintBoundary) {
		rb.debugLabel = label
	}
}

// NewRepaintBoundary creates a RepaintBoundary that caches the rendering
// of the given child widget.
//
// If child is nil, the boundary renders nothing and reports zero size.
//
// Options:
//   - [WithDebugLabel] — optional label for diagnostics
func NewRepaintBoundary(child widget.Widget, opts ...Option) *RepaintBoundary {
	rb := &RepaintBoundary{
		child: child,
	}
	rb.SetVisible(true)
	rb.SetEnabled(true)

	for _, opt := range opts {
		opt(rb)
	}

	return rb
}

// Child returns the wrapped child widget.
func (rb *RepaintBoundary) Child() widget.Widget {
	return rb.child
}

// DebugLabel returns the diagnostic label, or empty string if none set.
func (rb *RepaintBoundary) DebugLabel() string {
	return rb.debugLabel
}

// CacheHits returns how many times the cache was served instead of re-rendering.
func (rb *RepaintBoundary) CacheHits() int {
	return rb.cacheHits
}

// CacheValid reports whether the cache currently holds valid content.
func (rb *RepaintBoundary) CacheValid() bool {
	return rb.cacheValid
}

// InvalidateCache marks the cache as stale, forcing a re-render on the
// next draw pass. This is called automatically when descendants are dirty;
// manual invocation is rarely needed.
func (rb *RepaintBoundary) InvalidateCache() {
	rb.cacheValid = false
}

// --- widget.Widget interface ---

// Layout delegates to the child and stores the resulting size.
func (rb *RepaintBoundary) Layout(ctx widget.Context, constraints geometry.Constraints) geometry.Size {
	if rb.child == nil {
		size := constraints.Constrain(geometry.Sz(0, 0))
		rb.SetBounds(geometry.FromPointSize(rb.Position(), size))
		return size
	}

	size := rb.child.Layout(ctx, constraints)

	// Position child at origin (no offset within boundary).
	rb.child.(interface{ SetBounds(geometry.Rect) }).SetBounds(
		geometry.FromPointSize(geometry.Pt(0, 0), size),
	)

	rb.SetBounds(geometry.FromPointSize(rb.Position(), size))

	// Invalidate cache if size changed.
	w := int(size.Width)
	h := int(size.Height)
	if w != rb.cacheWidth || h != rb.cacheHeight {
		rb.cacheValid = false
		rb.cacheWidth = w
		rb.cacheHeight = h
	}

	return size
}

// Draw renders the child subtree, using the pixel cache when possible.
//
// If the child subtree is clean and the cache is valid, the cached image
// is composited directly. Otherwise, the child is rendered into an offscreen
// buffer, the result is captured as the new cache, and then composited.
func (rb *RepaintBoundary) Draw(ctx widget.Context, canvas widget.Canvas) {
	if !rb.IsVisible() {
		return
	}

	if rb.child == nil {
		return
	}

	bounds := rb.Bounds()
	w := int(bounds.Width())
	h := int(bounds.Height())
	if w <= 0 || h <= 0 {
		return
	}

	// Check if the child subtree needs redrawing.
	subtreeDirty := widget.NeedsRedrawInTree(rb.child)

	if rb.cacheValid && !subtreeDirty {
		// Cache hit: blit the cached image directly.
		rb.cacheHits++
		canvas.DrawImage(rb.cache, bounds.Min)
		return
	}

	// Cache miss: render child into offscreen context.
	rb.renderToCache(ctx, w, h)

	// Clear redraw flags in the child subtree since we just rendered them.
	widget.ClearRedrawInTree(rb.child)

	// Blit the freshly rendered cache.
	canvas.DrawImage(rb.cache, bounds.Min)
}

// renderToCache creates a temporary gg.Context, draws the child into it,
// and captures the result as an image.RGBA cache.
func (rb *RepaintBoundary) renderToCache(ctx widget.Context, w, h int) {
	// Create offscreen gg.Context.
	dc := gg.NewContext(w, h)

	// Wrap in Canvas for the widget system.
	offscreen := internalRender.NewCanvas(dc, w, h)

	// Clear with transparent background so the cache composites correctly.
	offscreen.Clear(widget.ColorTransparent)

	// Draw the child subtree into the offscreen canvas.
	// The child's bounds are at (0,0) relative to this boundary,
	// so no transform is needed.
	rb.child.Draw(ctx, offscreen)

	// Extract rendered pixels.
	img := dc.Image()

	// Convert to *image.RGBA for efficient compositing.
	rb.cache = toRGBA(img)
	rb.cacheValid = true

	// Close the temporary context to free resources.
	_ = dc.Close()
}

// toRGBA converts an image.Image to *image.RGBA efficiently.
// If the image is already *image.RGBA, it is returned directly.
func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)
	return rgba
}

// Event dispatches events to the child.
func (rb *RepaintBoundary) Event(ctx widget.Context, e event.Event) bool {
	if !rb.IsVisible() || !rb.IsEnabled() {
		return false
	}

	if rb.child == nil {
		return false
	}

	// Translate mouse events to local coordinates.
	if me, ok := e.(*event.MouseEvent); ok {
		local := *me
		local.Position = me.Position.Sub(rb.Bounds().Min)
		return rb.child.Event(ctx, &local)
	}

	return rb.child.Event(ctx, e)
}

// Children returns the child widget, or nil if none.
func (rb *RepaintBoundary) Children() []widget.Widget {
	if rb.child == nil {
		return nil
	}
	return []widget.Widget{rb.child}
}

// Unmount releases the pixel cache when the widget is removed from the tree.
func (rb *RepaintBoundary) Unmount() {
	rb.cache = nil
	rb.cacheValid = false
	rb.cacheWidth = 0
	rb.cacheHeight = 0
	rb.cacheHits = 0
}

// --- a11y.Accessible interface ---

// AccessibilityRole returns [a11y.RoleGenericContainer].
func (rb *RepaintBoundary) AccessibilityRole() a11y.Role {
	return a11y.RoleGenericContainer
}

// AccessibilityLabel returns the debug label or empty string.
func (rb *RepaintBoundary) AccessibilityLabel() string {
	return rb.debugLabel
}

// AccessibilityHint returns an empty string.
func (rb *RepaintBoundary) AccessibilityHint() string {
	return ""
}

// AccessibilityValue returns an empty string.
func (rb *RepaintBoundary) AccessibilityValue() string {
	return ""
}

// AccessibilityState returns the default state.
func (rb *RepaintBoundary) AccessibilityState() a11y.State {
	return a11y.State{
		Disabled: !rb.IsEnabled(),
		Hidden:   !rb.IsVisible(),
	}
}

// AccessibilityActions returns nil.
func (rb *RepaintBoundary) AccessibilityActions() []a11y.Action {
	return nil
}

// Compile-time interface checks.
var (
	_ widget.Widget   = (*RepaintBoundary)(nil)
	_ a11y.Accessible = (*RepaintBoundary)(nil)
)
