// Package menu works out what the menu-bar menu shows from the Companion's
// state. It has no UI in it: the app draws what For returns.
package menu

import "time"

// State is where the Companion stands, as far as the menu cares.
type State struct {
	// Now is the time, to say "today" and "yesterday".
	Now time.Time

	// Linked is whether the Companion is linked to an account.
	Linked bool
	// LastUpload is when the last upload went through, zero if none has.
	LastUpload time.Time
	// GameMissing is whether the game's folder can't be found.
	GameMissing bool
	// Unreachable is whether the last upload failed: the server couldn't be
	// reached or answered with an error.
	Unreachable bool
	// RestartNeeded is whether the data addon was first written while the
	// game was running, and that game is still running.
	RestartNeeded bool

	// GettingCode is whether the Companion is asking the server for a code.
	GettingCode bool
	// Code is the link code waiting to be confirmed on the website.
	Code string
	// CodeRanOut is whether the last code ran out before it was confirmed.
	CodeRanOut bool
	// LinkFailed is whether asking for a code failed.
	LinkFailed bool

	// StartAtLogin is whether the Companion is among the login items.
	StartAtLogin bool
}

// Menu is what the menu shows. Besides it, the menu always has "Start when
// I log in", "Open the website", "Show log" and "Quit".
type Menu struct {
	// Lines say what's going on; they can't be chosen.
	Lines []string
	// Link is whether "Link this computer…" is offered.
	Link bool
	// StartAtLogin is whether "Start when I log in" has its check mark.
	StartAtLogin bool
}

// For works out the menu for a state.
func For(s State) Menu {
	m := Menu{StartAtLogin: s.StartAtLogin}
	if !s.Linked {
		switch {
		case s.GettingCode:
			m.Lines = []string{"Not linked"}
		case s.Code != "":
			m.Lines = []string{"Not linked", "Code: " + s.Code}
		case s.CodeRanOut:
			m.Lines = []string{"The code ran out"}
			m.Link = true
		case s.LinkFailed:
			m.Lines = []string{"Not linked", "Can't reach PriceWatch Forever"}
			m.Link = true
		default:
			m.Lines = []string{"Not linked"}
			m.Link = true
		}
		return m
	}

	switch {
	case s.GameMissing:
		m.Lines = []string{"Can't find World of Warcraft: Forever"}
	case s.Unreachable:
		m.Lines = []string{"Can't reach PriceWatch Forever, trying again"}
	default:
		m.Lines = []string{"Linked"}
	}
	if s.RestartNeeded && !s.GameMissing {
		m.Lines = append(m.Lines, "Restart the game to finish setting up")
	}
	m.Lines = append(m.Lines, lastUpload(s.LastUpload, s.Now))
	return m
}

// lastUpload says when the last upload went through, in now's time zone.
func lastUpload(at, now time.Time) string {
	if at.IsZero() {
		return "No upload yet"
	}
	at = at.In(now.Location())
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()) }
	switch day(now).Sub(day(at)).Round(time.Hour) / (24 * time.Hour) {
	case 0:
		return "Last upload: today " + at.Format("15:04")
	case 1:
		return "Last upload: yesterday " + at.Format("15:04")
	default:
		return "Last upload: " + at.Format("Mon 2 Jan 15:04")
	}
}
