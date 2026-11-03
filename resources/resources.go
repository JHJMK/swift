// Package resources holds the pictures the app is built with.
package resources

import _ "embed"

// SwiftPNG is the app's icon, which the sidebar and the onboarding page
// show beside the app's name.
//
//go:embed swift.png
var SwiftPNG []byte
