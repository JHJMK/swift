package app

import (
	"github.com/egoist/mygo/ui"

	"swift/internal/app/icon"
)

var brandBitmap *ui.Bitmap

// brandLogo shows the app's icon, size DIPs square with corners of radius,
// or an empty box of that size if the icon cannot be decoded.
func brandLogo(c *ui.Context, size, radius float32) ui.Element {
	if brandBitmap == nil {
		if bmp, err := ui.DecodeBitmap(icon.SwiftPNG); err == nil {
			brandBitmap = bmp
		}
	}
	if brandBitmap == nil {
		return ui.Box(c).Width(size).Height(size)
	}
	return ui.Image(c, brandBitmap).Width(size).Height(size).Radius(radius)
}
