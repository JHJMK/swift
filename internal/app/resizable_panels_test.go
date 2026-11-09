package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFileTreeResizerDrag(t *testing.T) {
	tests := []struct {
		name  string
		delta float32
		want  float32
	}{
		{name: "wider", delta: 40, want: 260},
		{name: "maximum", delta: 200, want: maxFileTreeWidth},
		{name: "minimum", delta: -100, want: minFileTreeWidth},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a, _, rel := newTestApp(t)
			a.openDoc(rel)
			tt := ui.NewTester(a.view, 1180, 760)
			tt.Frame()

			rect, ok := tt.Find("调整文件树宽度")
			if !ok {
				t.Fatalf("file tree resizer is missing: %q", tt.Texts())
			}
			x, y := rect.X+rect.W/2, rect.Y+rect.H/2
			tt.Press(x, y)
			tt.Move(x+test.delta, y)
			tt.Frame()
			if got := a.fileTreeWidth(); got != test.want {
				t.Fatalf("width after drag = %v, want %v", got, test.want)
			}

			tt.Release(x+test.delta, y)
			tt.Frame()
			if got := loadConfig().FileTreeWidth; got != test.want {
				t.Fatalf("saved width = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOutlineWidthClampsAndSaves(t *testing.T) {
	a, _, _ := newTestApp(t)
	if got := a.outlineWidth(); got != defaultOutlineWidth {
		t.Fatalf("default outline width = %v, want %v", got, defaultOutlineWidth)
	}
	if err := a.setOutlineWidth(100); err != nil {
		t.Fatal(err)
	}
	if got := loadConfig().OutlineWidth; got != minOutlineWidth {
		t.Fatalf("saved minimum outline width = %v, want %v", got, minOutlineWidth)
	}
	if err := a.setOutlineWidth(500); err != nil {
		t.Fatal(err)
	}
	if got := loadConfig().OutlineWidth; got != maxOutlineWidth {
		t.Fatalf("saved maximum outline width = %v, want %v", got, maxOutlineWidth)
	}
}
