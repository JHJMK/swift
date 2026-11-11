package app

import (
	"bytes"
	"image/png"
	"testing"
)

func TestTrayIconIsPNG(t *testing.T) {
	b := trayIcon()
	if len(b) == 0 {
		t.Fatal("no tray icon")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 64 || cfg.Height != 64 {
		t.Fatalf("tray icon is %dx%d", cfg.Width, cfg.Height)
	}
}
