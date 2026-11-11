package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log"
	"runtime"

	"github.com/egoist/mygo"
)

// setupTray adds the menu bar or notification area icon that keeps 语燕
// reachable after its window is closed. Closing the window hides it; the
// tray menu is where the app quits for good.
func (a *app) setupTray() {
	if a.win == nil {
		return
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "显示语燕", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.showFromBackground()
		}},
		mygo.Separator(),
		{Label: "退出语燕", Click: func(*mygo.MenuItem, *mygo.Window) {
			mygo.App.Quit()
		}},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "main",
		Icon:           trayIcon(),
		IconIsTemplate: true,
		ToolTip:        "语燕",
		Menu:           menu,
	})
	if err != nil {
		log.Printf("swift: tray: %v", err)
		return
	}
	a.tray = tray
	// macOS opens the menu on a click of a menu bar item. Windows keeps
	// the menu for a right click, so a left click brings the window back.
	if runtime.GOOS == "windows" {
		tray.OnClick(a.showFromBackground)
	}
}

// showFromBackground shows the main window and the app again.
func (a *app) showFromBackground() {
	if a.win == nil {
		return
	}
	a.hideAfterFullScreen = false
	if runtime.GOOS == "darwin" {
		mygo.App.Show()
	}
	a.win.Show()
}

// trayIcon draws a small book, as a template image the system tints for
// its menu bar or taskbar.
func trayIcon() []byte {
	const size = 64
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	ink := color.NRGBA{A: 255}
	draw := func(x, y int) {
		if x >= 0 && x < size && y >= 0 && y < size {
			img.SetNRGBA(x, y, ink)
		}
	}
	fill := func(x0, y0, x1, y1 int) {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				draw(x, y)
			}
		}
	}
	// The covers' outline.
	fill(12, 10, 51, 13)
	fill(12, 52, 51, 55)
	fill(10, 13, 13, 52)
	fill(51, 13, 54, 52)
	// The spine and the lines on the page.
	fill(21, 15, 23, 52)
	fill(29, 23, 47, 23)
	fill(29, 33, 47, 33)
	fill(29, 43, 41, 43)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil
	}
	return b.Bytes()
}

// prepareQuit flushes the open page and the mirror once, before the
// window closes for good.
func (a *app) prepareQuit() {
	if a.quitting {
		return
	}
	a.quitting = true
	a.hideAfterFullScreen = false
	a.stopKBWatch()
	a.flushTitle()
	a.flushDoc()
	a.flushSync()
}

// hideToBackground hides the window but leaves the app and its sync
// running, with the tray icon to bring it back.
func (a *app) hideToBackground() {
	if a.win == nil {
		return
	}
	// Hiding is not quitting, but the page should still be on disk before
	// the window leaves the screen.
	a.flushTitle()
	a.flushDoc()
	// A macOS window hidden while it is in native full screen leaves a
	// black full-screen Space behind. Leave full screen first, then hide
	// the window once the system reports the transition.
	if a.win.IsFullScreen() {
		a.hideAfterFullScreen = true
		a.win.SetFullScreen(false)
		return
	}
	a.hideNow()
}

// leaveFullScreenForHide finishes a close that came from full screen.
func (a *app) leaveFullScreenForHide() {
	if !a.hideAfterFullScreen {
		return
	}
	a.hideAfterFullScreen = false
	a.hideNow()
}

func (a *app) hideNow() {
	if a.win == nil {
		return
	}
	a.win.Hide()
	if runtime.GOOS == "darwin" {
		mygo.App.Hide()
	}
}
