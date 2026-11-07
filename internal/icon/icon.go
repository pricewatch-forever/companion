// Package icon holds the Companion's icons, drawn by gen.
package icon

import _ "embed"

//go:generate go run ./gen

// MenuBar is the menu-bar icon, a template image: black on clear, which
// macOS colours to suit the menu bar.
//
//go:embed menubar.png
var MenuBar []byte

// app.png, the app icon, is made into the .app's icon by build-app.sh.
