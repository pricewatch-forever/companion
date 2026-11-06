// Command companion uploads what the PriceWatch addon records in World of
// Warcraft: Forever to the player's PriceWatch Forever account.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/pricewatch-forever/companion/internal/api"
	"github.com/pricewatch-forever/companion/internal/companion"
	"github.com/pricewatch-forever/companion/internal/config"
	"github.com/pricewatch-forever/companion/internal/wow"
)

// version is the Companion's own version, sent to the server.
var version = "0.1.0"

// defaultServer is the PriceWatch Forever server.
const defaultServer = "https://vibrant-bullfrog-841.eu-west-1.convex.site"

const usage = `PriceWatch Companion %s
Uploads what the PriceWatch addon records in World of Warcraft: Forever to
your PriceWatch Forever account.

Usage:
  companion link     link this computer to your account (once)
  companion run      upload now, then each time the game saves; leave it
                     running while you play, Ctrl-C stops it
  companion status   say whether it's linked and when it last uploaded

Flags:
  --game FOLDER   the game's Forever folder
                  (default "%s")
  --server URL    the server to link to (default: PriceWatch Forever's);
                  run uses the server it was linked to

Run "companion <command> --help" for a command's flags.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, usage, version, wow.DefaultInstall)
		return 2
	}
	cmd, args := args[0], args[1:]
	var err error
	switch cmd {
	case "link":
		err = linkCmd(args, stdout, stderr)
	case "run":
		err = runCmd(args, stdout, stderr)
	case "status":
		err = statusCmd(args, stdout, stderr)
	case "help", "-h", "-help", "--help":
		fmt.Fprintf(stdout, usage, version, wow.DefaultInstall)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, version)
		return 0
	default:
		fmt.Fprintf(stderr, "companion: no command %q\n\n", cmd)
		fmt.Fprintf(stderr, usage, version, wow.DefaultInstall)
		return 2
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errUsage):
		return 2
	case errors.Is(err, companion.ErrUnlinked), errors.Is(err, context.Canceled):
		// Already said.
		return 1
	default:
		fmt.Fprintf(stderr, "companion: %v\n", err)
		return 1
	}
}

var errUsage = errors.New("usage")

func newFlags(name, summary string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: companion %s [flags]\n%s\n", name, summary)
		n := 0
		fs.VisitAll(func(f *flag.Flag) {
			if n == 0 {
				fmt.Fprint(stderr, "\nFlags:\n")
			}
			n++
			arg, text := flag.UnquoteUsage(f)
			fmt.Fprintf(stderr, "  --%s %s\n      %s", f.Name, arg, text)
			if f.DefValue != "" {
				fmt.Fprintf(stderr, " (default %q)", f.DefValue)
			}
			fmt.Fprintln(stderr)
		})
	}
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(fs.Output(), "companion %s: unexpected %q\n", fs.Name(), fs.Arg(0))
		fs.Usage()
		return errUsage
	}
	return nil
}

func store() (*config.Store, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("can't find your config folder: %w", err)
	}
	return config.NewStore(path), nil
}

// isWebAddress reports whether addr is an http or https address.
func isWebAddress(addr string) bool {
	u, err := url.Parse(addr)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func checkServer(s string) (string, error) {
	if !isWebAddress(s) {
		return "", fmt.Errorf("--server %q isn't a web address", s)
	}
	return strings.TrimRight(s, "/"), nil
}

func linkCmd(args []string, stdout, stderr io.Writer) error {
	fs := newFlags("link", "Links this computer to your PriceWatch Forever account.", stderr)
	server := fs.String("server", defaultServer, "the `URL` of the server to link to")
	if err := parse(fs, args); err != nil {
		return err
	}
	base, err := checkServer(*server)
	if err != nil {
		return err
	}
	st, err := store()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	l := &companion.Linker{
		Client: api.New(base, version),
		Server: base,
		Store:  st,
		Clock:  companion.SystemClock{},
		Open:   openBrowser,
		Out:    stdout,
	}
	return l.Link(ctx, computerName())
}

func runCmd(args []string, stdout, stderr io.Writer) error {
	fs := newFlags("run", "Uploads the PriceWatch addon's data now, then each time the game saves it.", stderr)
	game := fs.String("game", wow.DefaultInstall, "the game's Forever `FOLDER`")
	server := fs.String("server", "", "the `URL` of the server it was linked to (default: that one)")
	if err := parse(fs, args); err != nil {
		return err
	}
	st, err := store()
	if err != nil {
		return err
	}
	cfg, err := st.Load()
	if err != nil {
		return err
	}
	if !cfg.Linked() {
		return companion.ErrNotLinked
	}
	base := cfg.Server
	if base == "" {
		base = defaultServer
	}
	if *server != "" {
		s, err := checkServer(*server)
		if err != nil {
			return err
		}
		if s != base {
			return fmt.Errorf("this computer is linked on %s, not %s: run `companion link --server %s` to link it there", base, s, s)
		}
	}
	if info, err := os.Stat(*game); err != nil || !info.IsDir() {
		return fmt.Errorf("no game folder at %s: give it with --game", *game)
	}
	if _, err := wow.Files(*game); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := &companion.Runner{
		Game:   *game,
		Client: api.New(base, version),
		Store:  st,
		Clock:  companion.SystemClock{},
		Out:    stdout,
	}
	events, err := wow.Watch(ctx, *game, func(err error) { r.Say("Watching the game's files: %v", err) })
	if err != nil {
		return fmt.Errorf("can't watch the game's files: %w", err)
	}
	r.Events = events
	fmt.Fprintf(stdout, "Uploading PriceWatch data from %s. Leave this running while you play; Ctrl-C stops it.\n", *game)
	return r.Run(ctx)
}

func statusCmd(args []string, stdout, stderr io.Writer) error {
	fs := newFlags("status", "Says whether this computer is linked and when it last uploaded.", stderr)
	if err := parse(fs, args); err != nil {
		return err
	}
	st, err := store()
	if err != nil {
		return err
	}
	cfg, err := st.Load()
	if err != nil {
		return err
	}
	if !cfg.Linked() {
		fmt.Fprintln(stdout, "Not linked. Run `companion link` to link this computer to your PriceWatch Forever account.")
		return nil
	}
	if cfg.Server != "" && cfg.Server != defaultServer {
		fmt.Fprintf(stdout, "Linked, on %s.\n", cfg.Server)
	} else {
		fmt.Fprintln(stdout, "Linked.")
	}
	if cfg.LastUpload.IsZero() {
		fmt.Fprintln(stdout, "Nothing uploaded yet.")
	} else {
		fmt.Fprintf(stdout, "Last upload: %s.\n", cfg.LastUpload.Local().Format("Mon 2 Jan 2006 15:04"))
	}
	return nil
}

// computerName is the name the website shows for this computer: its host
// name, without macOS's ".local".
func computerName() string {
	name, err := os.Hostname()
	if err != nil {
		name = ""
	}
	name = strings.TrimSpace(strings.TrimSuffix(strings.ToValidUTF8(name, ""), ".local"))
	for len(name) > 64 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if name == "" {
		name = "My computer"
	}
	return name
}

// openBrowser opens a web page in the default browser, on macOS. It opens
// only http and https addresses. Elsewhere it fails, and link shows the
// address instead.
var openBrowser = func(page string) error {
	if !isWebAddress(page) {
		return fmt.Errorf("not a web page: %q", page)
	}
	if runtime.GOOS != "darwin" {
		return errors.New("opening a browser is only done on macOS")
	}
	cmd := exec.Command("open", page)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
