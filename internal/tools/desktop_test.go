package tools

import (
	"reflect"
	"testing"
)

func TestDesktopCoordinateHelpers(t *testing.T) {
	x, y, ok := parsePair(" 12 , 34 ")
	if !ok || x != 12 || y != 34 {
		t.Fatalf("parsePair returned (%d,%d,%v), want (12,34,true)", x, y, ok)
	}
	if _, _, ok := parsePair("12x34"); ok {
		t.Fatalf("parsePair accepted invalid pair")
	}

	info := &desktopWindowInfo{X: 100, Y: 200, Width: 300, Height: 400}
	region := verifyRegionFromArgs(map[string]any{"verify_region": map[string]any{"x": 10, "y": 20, "width": 30, "height": 40, "space": "window"}}, info)
	if region == nil || region.X != 110 || region.Y != 220 || region.Width != 30 || region.Height != 40 {
		t.Fatalf("verifyRegionFromArgs window conversion = %#v", region)
	}
	if invalid := verifyRegionFromArgs(map[string]any{"verify_region": map[string]any{"width": 0, "height": 40}}, info); invalid != nil {
		t.Fatalf("verifyRegionFromArgs accepted invalid dimensions: %#v", invalid)
	}

	if !pointInsideWindow(desktopPoint{X: 299, Y: 399}, info) {
		t.Fatalf("pointInsideWindow rejected point inside window")
	}
	if pointInsideWindow(desktopPoint{X: 300, Y: 399}, info) {
		t.Fatalf("pointInsideWindow accepted point on exclusive right edge")
	}
}

func TestDesktopTimingAndDragHelpers(t *testing.T) {
	if got := boundedDesktopMS(-1, 10); got != 0 {
		t.Fatalf("boundedDesktopMS negative = %d, want 0", got)
	}
	if got := boundedDesktopMS(30, 10); got != 10 {
		t.Fatalf("boundedDesktopMS capped = %d, want 10", got)
	}

	cmds := buildDesktopDragCommands(map[string]any{"steps": 3, "duration_ms": 90, "hold_ms": 5, "release_wait_ms": 7}, desktopPoint{X: 0, Y: 0}, desktopPoint{X: 9, Y: 6})
	want := []string{"dd:0,0", "w:5", "m:3,2", "w:30", "m:6,4", "w:30", "m:9,6", "w:7", "du:9,6"}
	if !reflect.DeepEqual(cmds, want) {
		t.Fatalf("buildDesktopDragCommands = %#v, want %#v", cmds, want)
	}
}

func TestDesktopKeyboardHelpers(t *testing.T) {
	if got := normalizeCliclickKeys("Command + Shift + Return"); got != "cmd+shift+enter" {
		t.Fatalf("normalizeCliclickKeys = %q", got)
	}
	if got := cliclickKeyArgs("cmd+enter"); !reflect.DeepEqual(got, []string{"kd:cmd", "kp:enter", "ku:cmd"}) {
		t.Fatalf("cliclickKeyArgs cmd+enter = %#v", got)
	}
	if !desktopPreferClipboardTyping("中文") {
		t.Fatalf("desktopPreferClipboardTyping should prefer clipboard for non-ASCII text")
	}
	if !desktopPreferClipboardTyping("line1\nline2") {
		t.Fatalf("desktopPreferClipboardTyping should prefer clipboard for multiline text")
	}
	if desktopPreferClipboardTyping("short ascii") {
		t.Fatalf("desktopPreferClipboardTyping should not prefer clipboard for short ASCII")
	}
}
