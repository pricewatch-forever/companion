#!/bin/sh
# Builds "PriceWatch Companion.app", the menu-bar app, into dist/, for this
# Mac. Needs Go and Xcode's command line tools (xcode-select --install).
set -eu
cd "$(dirname "$0")"

app="dist/PriceWatch Companion.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

go build -o "$app/Contents/MacOS/PriceWatch Companion" .
version=$("$app/Contents/MacOS/PriceWatch Companion" version)

# The app icon, at every size macOS asks for.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
iconset="$work/AppIcon.iconset"
mkdir "$iconset"
for size in 16 32 128 256 512; do
	sips -z "$size" "$size" internal/icon/app.png --out "$iconset/icon_${size}x${size}.png" >/dev/null
	double=$((size * 2))
	sips -z "$double" "$double" internal/icon/app.png --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

# LSUIElement: in the menu bar only, never in the Dock.
cat >"$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>PriceWatch Companion</string>
	<key>CFBundleDisplayName</key>
	<string>PriceWatch Companion</string>
	<key>CFBundleIdentifier</key>
	<string>com.pricewatchforever.companion</string>
	<key>CFBundleExecutable</key>
	<string>PriceWatch Companion</string>
	<key>CFBundleIconFile</key>
	<string>AppIcon</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>$version</string>
	<key>CFBundleVersion</key>
	<string>$version</string>
	<key>LSMinimumSystemVersion</key>
	<string>11.0</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
PLIST

# Signed for this Mac only ("ad hoc"), which macOS needs to run it.
codesign --force --sign - "$app"

echo "Built $app ($version)"
