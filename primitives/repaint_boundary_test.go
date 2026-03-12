package primitives_test

import (
	"image"
	"testing"

	"github.com/gogpu/ui/a11y"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
)

// drawCountingWidget tracks how many times Draw is called.
type drawCountingWidget struct {
	widget.WidgetBase
	drawCount int
}

func newDrawCountingWidget() *drawCountingWidget {
	w := &drawCountingWidget{}
	w.SetVisible(true)
	w.SetEnabled(true)
	w.SetNeedsRedraw(true)
	return w
}

func (w *drawCountingWidget) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	return c.Constrain(geometry.Sz(100, 50))
}

func (w *drawCountingWidget) Draw(_ widget.Context, _ widget.Canvas) {
	w.drawCount++
}

func (w *drawCountingWidget) Event(_ widget.Context, _ event.Event) bool { return false }

var _ widget.Widget = (*drawCountingWidget)(nil)

// imageRecordingCanvas records DrawImage calls for validation.
type imageRecordingCanvas struct {
	mockCanvas
	drawImageCalls []drawImageCall
}

type drawImageCall struct {
	img image.Image
	at  geometry.Point
}

func (c *imageRecordingCanvas) DrawImage(img image.Image, at geometry.Point) {
	c.drawImageCalls = append(c.drawImageCalls, drawImageCall{img: img, at: at})
}

// --- Construction Tests ---

func TestNewRepaintBoundary_NilChild(t *testing.T) {
	rb := primitives.NewRepaintBoundary(nil)
	if rb == nil {
		t.Fatal("NewRepaintBoundary should never return nil")
	}
	if rb.Child() != nil {
		t.Error("expected nil child")
	}
	if rb.Children() != nil {
		t.Error("expected nil children slice for nil child")
	}
}

func TestNewRepaintBoundary_WithChild(t *testing.T) {
	child := primitives.Text("hello")
	rb := primitives.NewRepaintBoundary(child)

	if rb.Child() != child {
		t.Error("expected child to be returned")
	}
	children := rb.Children()
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if children[0] != child {
		t.Error("expected child in Children() slice")
	}
}

func TestNewRepaintBoundary_DefaultState(t *testing.T) {
	rb := primitives.NewRepaintBoundary(primitives.Text("x"))

	if !rb.IsVisible() {
		t.Error("should be visible by default")
	}
	if !rb.IsEnabled() {
		t.Error("should be enabled by default")
	}
	if rb.CacheValid() {
		t.Error("cache should not be valid initially")
	}
	if rb.CacheHits() != 0 {
		t.Error("cache hits should be 0 initially")
	}
	if rb.DebugLabel() != "" {
		t.Error("debug label should be empty by default")
	}
}

func TestNewRepaintBoundary_WithDebugLabel(t *testing.T) {
	rb := primitives.NewRepaintBoundary(nil, primitives.WithDebugLabel("chart"))
	if rb.DebugLabel() != "chart" {
		t.Errorf("expected debug label 'chart', got %q", rb.DebugLabel())
	}
}

// --- Layout Tests ---

func TestRepaintBoundary_Layout_NilChild(t *testing.T) {
	rb := primitives.NewRepaintBoundary(nil)
	constraints := geometry.Tight(geometry.Sz(200, 100))
	size := rb.Layout(nil, constraints)

	if size.Width != 200 || size.Height != 100 {
		t.Errorf("expected tight size 200x100, got %v", size)
	}
}

func TestRepaintBoundary_Layout_DelegatesToChild(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	constraints := geometry.BoxConstraints(0, 200, 0, 100)
	size := rb.Layout(nil, constraints)

	// drawCountingWidget returns Constrain(100, 50)
	if size.Width != 100 || size.Height != 50 {
		t.Errorf("expected 100x50, got %v", size)
	}
}

func TestRepaintBoundary_Layout_InvalidatesCacheOnSizeChange(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	// First layout
	rb.Layout(nil, geometry.BoxConstraints(0, 200, 0, 100))

	// Force a draw to populate cache
	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if !rb.CacheValid() {
		t.Error("cache should be valid after first draw")
	}

	// Change constraints to produce different size
	rb.Layout(nil, geometry.Tight(geometry.Sz(50, 25)))

	if rb.CacheValid() {
		t.Error("cache should be invalidated after size change")
	}
}

// --- Draw Tests ---

func TestRepaintBoundary_Draw_Invisible(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)
	rb.SetVisible(false)

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if child.drawCount > 0 {
		t.Error("invisible boundary should not draw child")
	}
	if len(canvas.drawImageCalls) > 0 {
		t.Error("invisible boundary should not call DrawImage")
	}
}

func TestRepaintBoundary_Draw_NilChild(t *testing.T) {
	rb := primitives.NewRepaintBoundary(nil)
	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if len(canvas.drawImageCalls) > 0 {
		t.Error("nil child should not call DrawImage")
	}
}

func TestRepaintBoundary_Draw_FirstDrawRendersChild(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	rb.Layout(nil, geometry.BoxConstraints(0, 200, 0, 100))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if child.drawCount != 1 {
		t.Errorf("expected child Draw called once, got %d", child.drawCount)
	}
	if len(canvas.drawImageCalls) != 1 {
		t.Fatalf("expected 1 DrawImage call, got %d", len(canvas.drawImageCalls))
	}
	if rb.CacheHits() != 0 {
		t.Error("first draw should not be a cache hit")
	}
	if !rb.CacheValid() {
		t.Error("cache should be valid after draw")
	}
}

func TestRepaintBoundary_Draw_SecondDrawUsesCacheWhenClean(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	rb.Layout(nil, geometry.BoxConstraints(0, 200, 0, 100))

	canvas := &imageRecordingCanvas{}
	// First draw: renders child, populates cache.
	rb.Draw(nil, canvas)

	// Child is now clean (ClearRedrawInTree called by RepaintBoundary.Draw).
	// Second draw should use cache.
	canvas2 := &imageRecordingCanvas{}
	rb.Draw(nil, canvas2)

	if child.drawCount != 1 {
		t.Errorf("expected child Draw called once total, got %d", child.drawCount)
	}
	if len(canvas2.drawImageCalls) != 1 {
		t.Fatalf("expected 1 DrawImage call on second draw, got %d", len(canvas2.drawImageCalls))
	}
	if rb.CacheHits() != 1 {
		t.Errorf("expected 1 cache hit, got %d", rb.CacheHits())
	}
}

func TestRepaintBoundary_Draw_DirtyChildInvalidatesCache(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	rb.Layout(nil, geometry.BoxConstraints(0, 200, 0, 100))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas) // First draw

	// Mark child dirty.
	child.SetNeedsRedraw(true)

	canvas2 := &imageRecordingCanvas{}
	rb.Draw(nil, canvas2) // Second draw: child dirty, must re-render.

	if child.drawCount != 2 {
		t.Errorf("expected child Draw called twice, got %d", child.drawCount)
	}
	if rb.CacheHits() != 0 {
		t.Errorf("expected 0 cache hits (dirty child), got %d", rb.CacheHits())
	}
}

func TestRepaintBoundary_Draw_ManualInvalidation(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	rb.Layout(nil, geometry.BoxConstraints(0, 200, 0, 100))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas) // First draw

	// Manually invalidate cache.
	rb.InvalidateCache()

	if rb.CacheValid() {
		t.Error("cache should be invalid after InvalidateCache")
	}

	canvas2 := &imageRecordingCanvas{}
	rb.Draw(nil, canvas2)

	if child.drawCount != 2 {
		t.Errorf("expected child Draw called twice after manual invalidation, got %d", child.drawCount)
	}
}

func TestRepaintBoundary_Draw_ZeroSizeSkipsRendering(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	// Set zero-size bounds.
	rb.SetBounds(geometry.NewRect(0, 0, 0, 0))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if child.drawCount > 0 {
		t.Error("zero-size boundary should not draw child")
	}
	if len(canvas.drawImageCalls) > 0 {
		t.Error("zero-size boundary should not call DrawImage")
	}
}

func TestRepaintBoundary_Draw_PositionPassedToDrawImage(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	rb.Layout(nil, geometry.BoxConstraints(0, 200, 0, 100))
	rb.SetBounds(geometry.NewRect(50, 30, 100, 50))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if len(canvas.drawImageCalls) != 1 {
		t.Fatalf("expected 1 DrawImage call, got %d", len(canvas.drawImageCalls))
	}

	at := canvas.drawImageCalls[0].at
	if at.X != 50 || at.Y != 30 {
		t.Errorf("expected DrawImage at (50,30), got (%v,%v)", at.X, at.Y)
	}
}

// --- Nested RepaintBoundary Tests ---

func TestRepaintBoundary_Nested(t *testing.T) {
	innerChild := newDrawCountingWidget()
	inner := primitives.NewRepaintBoundary(innerChild)

	outer := primitives.NewRepaintBoundary(inner)
	outer.Layout(nil, geometry.BoxConstraints(0, 200, 0, 100))

	canvas := &imageRecordingCanvas{}
	outer.Draw(nil, canvas) // First draw: both render.

	if innerChild.drawCount != 1 {
		t.Errorf("expected inner child Draw once, got %d", innerChild.drawCount)
	}

	// Second draw: outer serves from cache (inner is also clean).
	canvas2 := &imageRecordingCanvas{}
	outer.Draw(nil, canvas2)

	if innerChild.drawCount != 1 {
		t.Errorf("expected inner child Draw still 1 (outer cached), got %d", innerChild.drawCount)
	}
	if outer.CacheHits() != 1 {
		t.Errorf("expected outer cache hit, got %d", outer.CacheHits())
	}
}

// --- Event Tests ---

func TestRepaintBoundary_Event_DelegatesToChild(t *testing.T) {
	consumed := false
	child := &eventTestWidget{
		onEvent: func() { consumed = true },
	}
	child.SetVisible(true)
	child.SetEnabled(true)
	child.SetBounds(geometry.NewRect(0, 0, 100, 50))

	rb := primitives.NewRepaintBoundary(child)
	rb.SetBounds(geometry.NewRect(10, 10, 100, 50))

	// Send key event (non-mouse, no coordinate translation needed).
	ke := event.NewKeyEvent(event.KeyPress, event.KeyA, 'a', 0)
	result := rb.Event(nil, ke)

	if !consumed {
		t.Error("event should be dispatched to child")
	}
	if !result {
		t.Error("event should be consumed")
	}
}

func TestRepaintBoundary_Event_TranslatesMouseCoordinates(t *testing.T) {
	var receivedPos geometry.Point
	child := &mouseTrackingWidget{
		onMouse: func(pos geometry.Point) { receivedPos = pos },
	}
	child.SetVisible(true)
	child.SetEnabled(true)
	child.SetBounds(geometry.NewRect(0, 0, 100, 50))

	rb := primitives.NewRepaintBoundary(child)
	rb.SetBounds(geometry.NewRect(20, 30, 100, 50))

	pos := geometry.Pt(50, 40)
	me := event.NewMouseEvent(event.MousePress, event.ButtonLeft, 0, pos, pos, 0)
	rb.Event(nil, me)

	// Mouse position should be translated: (50-20, 40-30) = (30, 10)
	if receivedPos.X != 30 || receivedPos.Y != 10 {
		t.Errorf("expected translated position (30,10), got (%v,%v)", receivedPos.X, receivedPos.Y)
	}
}

func TestRepaintBoundary_Event_InvisibleIgnoresEvents(t *testing.T) {
	consumed := false
	child := &eventTestWidget{
		onEvent: func() { consumed = true },
	}
	child.SetVisible(true)
	child.SetEnabled(true)

	rb := primitives.NewRepaintBoundary(child)
	rb.SetVisible(false)

	ke := event.NewKeyEvent(event.KeyPress, event.KeyA, 'a', 0)
	rb.Event(nil, ke)

	if consumed {
		t.Error("invisible boundary should not dispatch events")
	}
}

func TestRepaintBoundary_Event_NilChild(t *testing.T) {
	rb := primitives.NewRepaintBoundary(nil)
	ke := event.NewKeyEvent(event.KeyPress, event.KeyA, 'a', 0)
	result := rb.Event(nil, ke)

	if result {
		t.Error("nil child should not consume events")
	}
}

// --- Unmount Tests ---

func TestRepaintBoundary_Unmount_FreesCache(t *testing.T) {
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	rb.Layout(nil, geometry.BoxConstraints(0, 200, 0, 100))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if !rb.CacheValid() {
		t.Error("cache should be valid after draw")
	}

	rb.Unmount()

	if rb.CacheValid() {
		t.Error("cache should be invalid after Unmount")
	}
	if rb.CacheHits() != 0 {
		t.Error("cache hits should be reset after Unmount")
	}
}

// --- Accessibility Tests ---

func TestRepaintBoundary_Accessibility(t *testing.T) {
	rb := primitives.NewRepaintBoundary(nil, primitives.WithDebugLabel("chart"))

	acc, ok := interface{}(rb).(a11y.Accessible)
	if !ok {
		t.Fatal("RepaintBoundary should implement a11y.Accessible")
	}

	if acc.AccessibilityRole() != a11y.RoleGenericContainer {
		t.Errorf("expected RoleGenericContainer, got %v", acc.AccessibilityRole())
	}
	if acc.AccessibilityLabel() != "chart" {
		t.Errorf("expected label 'chart', got %q", acc.AccessibilityLabel())
	}
	if acc.AccessibilityHint() != "" {
		t.Error("expected empty hint")
	}
	if acc.AccessibilityValue() != "" {
		t.Error("expected empty value")
	}

	state := acc.AccessibilityState()
	if state.Disabled {
		t.Error("should not be disabled by default")
	}
	if state.Hidden {
		t.Error("should not be hidden by default")
	}
	if acc.AccessibilityActions() != nil {
		t.Error("should have no actions")
	}
}

// --- Helper test widgets ---

type eventTestWidget struct {
	widget.WidgetBase
	onEvent func()
}

func (w *eventTestWidget) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	return c.Constrain(geometry.Sz(100, 50))
}

func (w *eventTestWidget) Draw(_ widget.Context, _ widget.Canvas) {}

func (w *eventTestWidget) Event(_ widget.Context, _ event.Event) bool {
	if w.onEvent != nil {
		w.onEvent()
	}
	return true
}

var _ widget.Widget = (*eventTestWidget)(nil)

type mouseTrackingWidget struct {
	widget.WidgetBase
	onMouse func(geometry.Point)
}

func (w *mouseTrackingWidget) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	return c.Constrain(geometry.Sz(100, 50))
}

func (w *mouseTrackingWidget) Draw(_ widget.Context, _ widget.Canvas) {}

func (w *mouseTrackingWidget) Event(_ widget.Context, e event.Event) bool {
	if me, ok := e.(*event.MouseEvent); ok {
		if w.onMouse != nil {
			w.onMouse(me.Position)
		}
		return true
	}
	return false
}

var _ widget.Widget = (*mouseTrackingWidget)(nil)
