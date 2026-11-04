// Package icon holds the app's icon, embedded in the binary.
package icon

import _ "embed"

// SwiftPNG is the app's icon, which the sidebar and the onboarding page
// show beside the app's name.
//
//go:embed swift.png
var SwiftPNG []byte
