// Package loginitem starts the Companion when the user logs in. On macOS it
// is a LaunchAgent: a small file in ~/Library/LaunchAgents that launchd
// reads at login.
package loginitem

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pricewatch-forever/companion/internal/atomicfile"
)

// LaunchAgent is the login item that runs Program at login.
type LaunchAgent struct {
	// Dir is the LaunchAgents folder, ~/Library/LaunchAgents.
	Dir string
	// Label names the agent; its file is <Label>.plist.
	Label string
	// Program is what it runs, with its arguments.
	Program []string
}

func (a LaunchAgent) path() string { return filepath.Join(a.Dir, a.Label+".plist") }

// Enabled reports whether the agent is there.
func (a LaunchAgent) Enabled() bool {
	_, err := os.Stat(a.path())
	return err == nil
}

// Enable adds the agent. It runs Program at the next login, not now.
func (a LaunchAgent) Enable() error {
	if err := os.MkdirAll(a.Dir, 0o755); err != nil {
		return err
	}
	return atomicfile.Write(a.path(), a.plist(), 0o644)
}

// Disable removes the agent, if it is there.
func (a LaunchAgent) Disable() error {
	err := os.Remove(a.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (a LaunchAgent) plist() []byte {
	var b bytes.Buffer
	str := func(s string) {
		b.WriteString("<string>")
		_ = xml.EscapeText(&b, []byte(s))
		b.WriteString("</string>")
	}
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	`)
	str(a.Label)
	b.WriteString("\n\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range a.Program {
		b.WriteString("\t\t")
		str(arg)
		b.WriteString("\n")
	}
	b.WriteString("\t</array>\n\t<key>RunAtLoad</key>\n\t<true/>\n</dict>\n</plist>\n")
	return b.Bytes()
}
