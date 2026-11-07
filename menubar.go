package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/pricewatch-forever/companion/internal/app"
	"github.com/pricewatch-forever/companion/internal/companion"
	"github.com/pricewatch-forever/companion/internal/config"
	"github.com/pricewatch-forever/companion/internal/icon"
	"github.com/pricewatch-forever/companion/internal/logfile"
	"github.com/pricewatch-forever/companion/internal/loginitem"
	"github.com/pricewatch-forever/companion/internal/menu"
	"github.com/pricewatch-forever/companion/internal/wow"
)

// website is PriceWatch Forever's website, for "Open the website".
const website = "https://pricewatchforever.com"

// launchAgentLabel names the login item, as the .app's bundle ID does.
const launchAgentLabel = "com.pricewatchforever.companion"

// appCmd runs the menu-bar app until Quit is chosen. A second copy quits at
// once.
func appCmd(args []string, stdout, stderr io.Writer) error {
	fs := newFlags("app", "Runs the Companion in the menu bar, as PriceWatch Companion.app does.", stderr)
	game := fs.String("game", wow.DefaultInstall, "the game's Forever `FOLDER`")
	server := fs.String("server", defaultServer, "the `URL` of the server to link to; once linked, it uploads to the server it was linked to")
	if err := parse(fs, args); err != nil {
		return err
	}
	base, err := checkServer(*server)
	if err != nil {
		return err
	}
	configPath, err := config.DefaultPath()
	if err != nil {
		return fmt.Errorf("can't find your config folder: %w", err)
	}
	logPath, err := logfile.DefaultPath()
	if err != nil {
		return fmt.Errorf("can't find your log folder: %w", err)
	}
	login, err := newLoginItem(*game)
	if err != nil {
		return err
	}

	log, err := logfile.Open(logPath, logfile.MaxSize)
	if err != nil {
		return fmt.Errorf("can't open the log: %w", err)
	}
	defer log.Close()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	ui := &menuBar{}
	a := &app.App{
		Game:          *game,
		Server:        base,
		Website:       website,
		Version:       version,
		Name:          computerName(),
		Store:         config.NewStore(configPath),
		Clock:         companion.SystemClock{},
		Tick:          ticker.C,
		Open:          openBrowser,
		LoginItem:     login,
		GameProcesses: wow.Running,
		Log:           log,
		Changed:       ui.draw,
		LockFile:      filepath.Join(filepath.Dir(configPath), "companion.lock"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	switch err := a.Start(ctx); {
	case errors.Is(err, app.ErrAlreadyRunning):
		fmt.Fprintln(stdout, "The Companion is already running, in the menu bar.")
		return nil
	case err != nil:
		fmt.Fprintf(log, "%s  Couldn't start: %v\n", time.Now().Format("2006-01-02 15:04:05"), err)
		return err
	}
	ui.app = a
	ui.logPath = log.Path()
	systray.Run(ui.ready, func() {})
	cancel()
	a.Wait()
	return nil
}

// menuBar draws the App's menu in the menu bar.
type menuBar struct {
	app     *app.App
	logPath string

	mu    sync.Mutex
	lines []*systray.MenuItem
	link  *systray.MenuItem
	login *systray.MenuItem
}

// maxLines is the most status lines menu.For gives.
const maxLines = 3

func (b *menuBar) ready() {
	systray.SetTemplateIcon(icon.MenuBar, icon.MenuBar)
	systray.SetTooltip("PriceWatch Companion")
	b.mu.Lock()
	for range maxLines {
		item := systray.AddMenuItem("", "")
		item.Disable()
		b.lines = append(b.lines, item)
	}
	b.link = systray.AddMenuItem("Link this computer…", "")
	systray.AddSeparator()
	b.login = systray.AddMenuItemCheckbox("Start when I log in", "", false)
	website := systray.AddMenuItem("Open the website", "")
	showLog := systray.AddMenuItem("Show log", "")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "")
	b.mu.Unlock()
	b.draw(b.app.Menu())

	go func() {
		for {
			select {
			case <-b.link.ClickedCh:
				b.app.Link()
			case <-b.login.ClickedCh:
				b.app.SetStartAtLogin(!b.app.Menu().StartAtLogin)
			case <-website.ClickedCh:
				b.app.OpenWebsite()
			case <-showLog.ClickedCh:
				if err := openWithSystem(b.logPath); err != nil {
					fmt.Fprintf(os.Stderr, "companion: can't show the log: %v\n", err)
				}
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// draw shows m. Before the menu is made, it does nothing: ready draws it.
func (b *menuBar) draw(m menu.Menu) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.link == nil {
		return
	}
	for i, item := range b.lines {
		if i < len(m.Lines) {
			item.SetTitle(m.Lines[i])
			item.Show()
		} else {
			item.Hide()
		}
	}
	if m.Link {
		b.link.Show()
	} else {
		b.link.Hide()
	}
	if m.StartAtLogin {
		b.login.Check()
	} else {
		b.login.Uncheck()
	}
}

// newLoginItem is the login item that starts this program at login: on
// macOS, a LaunchAgent that opens the .app, or runs `companion app` when
// not in one.
func newLoginItem(game string) (app.LoginItem, error) {
	if runtime.GOOS != "darwin" {
		return unsupportedLoginItem{}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	var flags []string
	if game != wow.DefaultInstall {
		flags = []string{"--game", game}
	}
	program := append([]string{exe, "app"}, flags...)
	if bundle, ok := appBundle(exe); ok {
		program = []string{"/usr/bin/open", bundle}
		if flags != nil {
			program = append(append(program, "--args"), flags...)
		}
	}
	return loginitem.LaunchAgent{
		Dir:     filepath.Join(home, "Library", "LaunchAgents"),
		Label:   launchAgentLabel,
		Program: program,
	}, nil
}

type unsupportedLoginItem struct{}

func (unsupportedLoginItem) Enabled() bool { return false }
func (unsupportedLoginItem) Enable() error {
	return errors.New("starting at login isn't done on this system yet")
}
func (unsupportedLoginItem) Disable() error { return nil }

// appBundle returns the .app a program is in, if it is in one.
func appBundle(exe string) (string, bool) {
	bundle, _, ok := strings.Cut(exe, ".app/Contents/MacOS/")
	if !ok {
		return "", false
	}
	return bundle + ".app", true
}
