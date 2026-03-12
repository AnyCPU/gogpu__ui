package primitives_test

import (
	"testing"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
)

// --- Scene Threshold Tests ---

func TestRepaintBoundary_SceneThreshold_SmallUsesContext(t *testing.T) {
	// Widget smaller than 128x128 should use gg.Context path (no scene resources).
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	// Layout to 64x64 = 4096 pixels (below threshold of 16384).
	rb.Layout(nil, geometry.Tight(geometry.Sz(64, 64)))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if child.drawCount != 1 {
		t.Errorf("expected child Draw called once, got %d", child.drawCount)
	}
	if !rb.CacheValid() {
		t.Error("cache should be valid after draw")
	}
	if len(canvas.drawImageCalls) != 1 {
		t.Fatalf("expected 1 DrawImage call, got %d", len(canvas.drawImageCalls))
	}
}

func TestRepaintBoundary_SceneThreshold_LargeUsesScene(t *testing.T) {
	// Widget at or above 128x128 should use scene.Renderer path.
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	// Layout to 128x128 = 16384 pixels (at threshold).
	rb.Layout(nil, geometry.Tight(geometry.Sz(128, 128)))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if child.drawCount != 1 {
		t.Errorf("expected child Draw called once, got %d", child.drawCount)
	}
	if !rb.CacheValid() {
		t.Error("cache should be valid after draw")
	}
	if len(canvas.drawImageCalls) != 1 {
		t.Fatalf("expected 1 DrawImage call, got %d", len(canvas.drawImageCalls))
	}

	// Verify the cached image has correct dimensions.
	img := canvas.drawImageCalls[0].img
	bounds := img.Bounds()
	if bounds.Dx() != 128 || bounds.Dy() != 128 {
		t.Errorf("expected cache image 128x128, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestRepaintBoundary_SceneRendering_LargeWidget(t *testing.T) {
	// A 256x256 widget should use scene path and produce valid output.
	child := &colorFillWidget{color: widget.ColorRed}
	child.SetVisible(true)
	child.SetEnabled(true)
	child.SetNeedsRedraw(true)

	rb := primitives.NewRepaintBoundary(child)
	rb.Layout(nil, geometry.Tight(geometry.Sz(256, 256)))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if !rb.CacheValid() {
		t.Error("cache should be valid after scene rendering")
	}
	if len(canvas.drawImageCalls) != 1 {
		t.Fatalf("expected 1 DrawImage call, got %d", len(canvas.drawImageCalls))
	}

	img := canvas.drawImageCalls[0].img
	bounds := img.Bounds()
	if bounds.Dx() != 256 || bounds.Dy() != 256 {
		t.Errorf("expected 256x256 image, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestRepaintBoundary_SceneReuse(t *testing.T) {
	// scene.Renderer and scene.Scene should be reused across frames.
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	rb.Layout(nil, geometry.Tight(geometry.Sz(200, 200)))

	// First draw: creates scene resources.
	canvas1 := &imageRecordingCanvas{}
	rb.Draw(nil, canvas1)

	if child.drawCount != 1 {
		t.Errorf("expected 1 draw after first frame, got %d", child.drawCount)
	}

	// Mark child dirty for a second render.
	child.SetNeedsRedraw(true)

	canvas2 := &imageRecordingCanvas{}
	rb.Draw(nil, canvas2)

	if child.drawCount != 2 {
		t.Errorf("expected 2 draws after dirty second frame, got %d", child.drawCount)
	}

	// Both frames should produce valid images.
	if len(canvas1.drawImageCalls) != 1 || len(canvas2.drawImageCalls) != 1 {
		t.Error("both frames should produce DrawImage calls")
	}
}

func TestRepaintBoundary_SceneResize(t *testing.T) {
	// Scene resources should be recreated when size changes.
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	// First frame: 200x200 (above threshold).
	rb.Layout(nil, geometry.Tight(geometry.Sz(200, 200)))
	canvas1 := &imageRecordingCanvas{}
	rb.Draw(nil, canvas1)

	if !rb.CacheValid() {
		t.Error("cache should be valid after first draw")
	}

	// Resize to 300x300: should invalidate cache and recreate resources.
	rb.Layout(nil, geometry.Tight(geometry.Sz(300, 300)))
	if rb.CacheValid() {
		t.Error("cache should be invalid after size change")
	}

	child.SetNeedsRedraw(true)
	canvas2 := &imageRecordingCanvas{}
	rb.Draw(nil, canvas2)

	if !rb.CacheValid() {
		t.Error("cache should be valid after re-render")
	}
	if len(canvas2.drawImageCalls) != 1 {
		t.Fatalf("expected 1 DrawImage call, got %d", len(canvas2.drawImageCalls))
	}

	img := canvas2.drawImageCalls[0].img
	bounds := img.Bounds()
	if bounds.Dx() != 300 || bounds.Dy() != 300 {
		t.Errorf("expected 300x300 image after resize, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestRepaintBoundary_SceneUnmount(t *testing.T) {
	// All scene resources should be freed on Unmount.
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	// Use large widget to trigger scene path.
	rb.Layout(nil, geometry.Tight(geometry.Sz(256, 256)))

	canvas := &imageRecordingCanvas{}
	rb.Draw(nil, canvas)

	if !rb.CacheValid() {
		t.Error("cache should be valid before unmount")
	}

	rb.Unmount()

	if rb.CacheValid() {
		t.Error("cache should be invalid after Unmount")
	}
	if rb.CacheHits() != 0 {
		t.Error("cache hits should be reset after Unmount")
	}
}

func TestRepaintBoundary_SceneCacheHit(t *testing.T) {
	// When cache is valid and child is clean, scene path should still serve
	// from cache (no re-render).
	child := newDrawCountingWidget()
	rb := primitives.NewRepaintBoundary(child)

	rb.Layout(nil, geometry.Tight(geometry.Sz(200, 200)))

	// First draw: renders via scene.
	canvas1 := &imageRecordingCanvas{}
	rb.Draw(nil, canvas1)

	// Second draw: child is clean, should use cache.
	canvas2 := &imageRecordingCanvas{}
	rb.Draw(nil, canvas2)

	if child.drawCount != 1 {
		t.Errorf("expected child Draw called once (cached on second), got %d", child.drawCount)
	}
	if rb.CacheHits() != 1 {
		t.Errorf("expected 1 cache hit, got %d", rb.CacheHits())
	}
}

// --- Helper test widget that fills with a color ---

// colorFillWidget draws a solid color fill across its bounds.
type colorFillWidget struct {
	widget.WidgetBase
	color widget.Color
}

func (w *colorFillWidget) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	return c.Constrain(geometry.Sz(256, 256))
}

func (w *colorFillWidget) Draw(_ widget.Context, canvas widget.Canvas) {
	canvas.DrawRect(geometry.NewRect(0, 0, 256, 256), w.color)
}

func (w *colorFillWidget) Event(_ widget.Context, _ event.Event) bool {
	return false
}

var _ widget.Widget = (*colorFillWidget)(nil)
