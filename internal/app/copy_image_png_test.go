package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// Copying a picture re-encodes the formats the clipboard does not take,
// and passes PNG through unchanged.
func TestImageToPNG(t *testing.T) {
	raw := testPNG(t)
	got, err := imageToPNG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("png data was re-encoded needlessly")
	}

	jpegImg := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			jpegImg.Set(i, j, color.RGBA{R: 200, G: 10, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, jpegImg); err != nil {
		t.Fatal(err)
	}
	pngBytes, err := imageToPNG(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 4 {
		t.Fatalf("re-encoded picture is %v", img.Bounds())
	}
	if _, err := imageToPNG([]byte("不是图片")); err == nil {
		t.Fatal("garbage accepted as a picture")
	}
}
