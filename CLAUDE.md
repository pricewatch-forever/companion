# PriceWatch Companion

A Go program players run beside World of Warcraft: Forever. It links to a PriceWatch Forever account, uploads `PriceWatchDB.recording` from each account's `PriceWatch.lua`, and writes the `PriceWatch_Companion` data addon. README.md says what it does for players.

## Fixtures are a contract

`fixtures/` is copied in from the server's repository; `fixtures/SOURCE.json` names the commit. Never edit a fixture here, not even to fix a typo: if a test disagrees with a fixture, the code is wrong, or the fixture must change at its source and be copied in again (with a new `SOURCE.json`).

- `companion-api.json`: the requests the Companion sends and the answers it reads.
- `saved-variables.json`: the SavedVariables parser's cases.
- `recording-v1.json`: what the recording holds; `format.companion` is the data addon's contract.

## Layout

- `main.go`: the `link`, `run` and `status` commands.
- `menubar.go`: the `app` command, the menu-bar app; the .app runs it with no command. It holds the systray drawing and the wiring to macOS (the login item's program, the .app's path); the app's behaviour lives in `internal/app`.
- `build-app.sh`: builds `dist/PriceWatch Companion.app`.
- `internal/app`: the menu-bar app without its UI: linking, keeping the upload loop running, the state the menu is worked out from.
- `internal/single`: only one copy of the app runs (a lock on a file).
- `internal/menu`: what the menu shows, worked out from the state by one function.
- `internal/savedvars`: the SavedVariables parser. It never runs the file.
- `internal/api`: the HTTP client for `/companion/v1`.
- `internal/companion`: linking, and the upload loop (injectable clock and file events). `internal/clocktest` is the tests' clock.
- `internal/addon`: writes the data addon.
- `internal/config`: the config file (mode 0600) that holds the device token.
- `internal/wow`: finds and watches the game's files, and its running processes.
- `internal/loginitem`: "Start when I log in", a LaunchAgent on macOS.
- `internal/logfile`: the app's log, rotated.
- `internal/icon`: the icons, drawn by `go generate ./internal/icon`.
- `internal/atomicfile`: replaces a file whole (the config file, the data addon, the login item).

## Rules

- Never print or log the device token. `api.Token` prints as `[hidden]`; keep the token in that type everywhere.
- Touch no game file but `Interface/AddOns/PriceWatch_Companion/`. Tests use temp folders only.
- Dependencies: the standard library, `fsnotify`, `fyne.io/systray` (the menu bar) and `golang.org/x/sys`. Ask before adding one.
- The menu never talks about tokens, files or formats: it says what's going on in plain words. The log may say more.
- `gofmt`, `go vet ./...` and `go test ./...` stay clean.
- Commits: plain, descriptive messages, and no AI attribution of any kind (no `Co-Authored-By` trailer, no "Generated with" line).
