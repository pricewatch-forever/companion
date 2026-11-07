package menu_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/pricewatch-forever/companion/internal/menu"
)

func TestTheMenuSaysWhatIsGoingOn(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 22, 30, 0, 0, berlin)
	today := time.Date(2026, 11, 7, 20, 14, 0, 0, time.UTC) // 21:14 in Berlin
	yesterday := time.Date(2026, 11, 6, 8, 5, 0, 0, berlin)
	earlier := time.Date(2026, 11, 2, 18, 0, 0, 0, berlin)

	for name, c := range map[string]struct {
		state menu.State
		want  menu.Menu
	}{
		"linked, uploaded today": {
			menu.State{Linked: true, LastUpload: today, StartAtLogin: true},
			menu.Menu{Lines: []string{"Linked", "Last upload: today 21:14"}, StartAtLogin: true},
		},
		"linked, uploaded yesterday": {
			menu.State{Linked: true, LastUpload: yesterday},
			menu.Menu{Lines: []string{"Linked", "Last upload: yesterday 08:05"}},
		},
		"linked, uploaded earlier": {
			menu.State{Linked: true, LastUpload: earlier},
			menu.Menu{Lines: []string{"Linked", "Last upload: Mon 2 Nov 18:00"}},
		},
		"linked, no upload yet": {
			menu.State{Linked: true},
			menu.Menu{Lines: []string{"Linked", "No upload yet"}},
		},
		// After a 401 the Companion forgets its link, so being unlinked on
		// the website reads the same.
		"not linked, or unlinked on the website": {
			menu.State{},
			menu.Menu{Lines: []string{"Not linked"}, Link: true},
		},
		"asking the server for a code": {
			menu.State{GettingCode: true},
			menu.Menu{Lines: []string{"Not linked"}},
		},
		"a code waiting": {
			menu.State{Code: "K7QM-R4TX"},
			menu.Menu{Lines: []string{"Not linked", "Code: K7QM-R4TX"}},
		},
		"the code ran out": {
			menu.State{CodeRanOut: true},
			menu.Menu{Lines: []string{"The code ran out"}, Link: true},
		},
		"no code, the server unreachable": {
			menu.State{LinkFailed: true},
			menu.Menu{Lines: []string{"Not linked", "Can't reach PriceWatch Forever"}, Link: true},
		},
		"no game folder": {
			menu.State{Linked: true, GameMissing: true, LastUpload: today},
			menu.Menu{Lines: []string{"Can't find World of Warcraft: Forever", "Last upload: today 21:14"}},
		},
		"the server unreachable": {
			menu.State{Linked: true, Unreachable: true, LastUpload: today},
			menu.Menu{Lines: []string{"Can't reach PriceWatch Forever, trying again", "Last upload: today 21:14"}},
		},
		"the server unreachable before any upload": {
			menu.State{Linked: true, Unreachable: true},
			menu.Menu{Lines: []string{"Can't reach PriceWatch Forever, trying again", "No upload yet"}},
		},
		"restart needed, the server unreachable": {
			menu.State{Linked: true, RestartNeeded: true, Unreachable: true},
			menu.Menu{Lines: []string{"Can't reach PriceWatch Forever, trying again", "Restart the game to finish setting up", "No upload yet"}},
		},
		"restart needed": {
			menu.State{Linked: true, RestartNeeded: true},
			menu.Menu{Lines: []string{"Linked", "Restart the game to finish setting up", "No upload yet"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			c.state.Now = now
			if got := menu.For(c.state); !reflect.DeepEqual(got, c.want) {
				t.Errorf("menu %#v\nwant %#v", got, c.want)
			}
		})
	}
}
