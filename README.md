# PriceWatch Companion

A desktop companion for the PriceWatch addon in World of Warcraft: Forever. Link it to your [PriceWatch Forever](https://pricewatchforever.com) account and your play sessions show up on the website: where you went, what you looted and what it was worth.

It's free, with nothing behind a payment.

**Status:** not released yet. You can build it from source and run it on macOS.

It lives in the menu bar: an icon, no window. Its menu says whether it's linked and when it last uploaded, links your computer to your account, and can start it whenever you log in, so you link once and never think about it again.

## What it touches on your computer

The Companion is open source so you can check exactly what it does:

- **It reads one file per WoW account:** the PriceWatch addon's saved data, `PriceWatch.lua`, in your game folder under `WTF/Account/<account>/SavedVariables/`. It never runs that file and never changes it.
- **It writes one small addon of its own:** `PriceWatch_Companion`, in your game folder under `Interface/AddOns/`, which tells the PriceWatch addon that the Companion is linked and what it has already sent, so the addon can clear out what's been sent. It changes no other game file.
- **It keeps its link to your account in its own settings file,** outside the game folder, readable only by you: on macOS, `~/Library/Application Support/PriceWatch Companion/config.json`. Beside it, an empty `companion.lock` makes sure only one copy runs.
- **It keeps a log of what it did,** for when something goes wrong, in `~/Library/Logs/PriceWatch Companion/`. The log stays small (once full, it replaces the older one) and never holds the link to your account. "Show log" in the menu opens it.
- **With "Start when I log in" on, it adds itself to your login items,** as `~/Library/LaunchAgents/com.pricewatchforever.companion.plist`, which opens the app when you log in. It's on once you link; turning it off in the menu removes the file. macOS lists it in System Settings under General → Login Items, among the items allowed in the background.
- **It sends your sessions to your PriceWatch Forever account only.** They're private to you and kept until you delete them. When linking, it also sends your computer's name, so the website can show you which computer is linked, and with each request its own version number.
- **It never talks to Blizzard** and holds no Blizzard keys.

## Build and run

You need a Mac, [Go](https://go.dev/dl/) 1.24 or later, and Xcode's command line tools (`xcode-select --install` in a terminal installs them).

```sh
git clone https://github.com/pricewatch-forever/companion.git
cd companion
./build-app.sh
```

That makes `PriceWatch Companion.app` in the `dist` folder. Move it to your Applications folder and open it. Its icon, a pocket watch, appears in the menu bar.

**Link it to your account, once:** choose "Link this computer…" in its menu. It opens the PriceWatch Forever website with a code, which the menu shows too. Sign in there and confirm the code. The menu then says "Linked".

**Then play.** It uploads what PriceWatch has recorded so far, then again each time the game saves it (when you log out or `/reload`). The first time, it adds its own small addon to the game. The game only finds a new addon when it starts, so if the game is running then, the menu says to restart it: a `/reload` isn't enough.

If you unlink the computer on the website, the menu says "Not linked" and the Companion stops uploading until you link it again.

If your game isn't in `/Applications/World of Warcraft/_forever_`, the menu says it can't find it. Start the app from a terminal with the game's folder:

```sh
open "/Applications/PriceWatch Companion.app" --args --game "/path/to/World of Warcraft/_forever_"
```

### In a terminal

The same program also runs in a terminal, sharing the app's settings: linking in one links the other. `go build` makes it as `companion` in the folder.

- `./companion link` links this computer: it shows a code and opens the website.
- `./companion run` uploads while you play. Leave it running in its terminal window; Ctrl-C stops it. Give the game's folder with `--game` if it isn't the usual one.
- `./companion status` says whether it's linked and when it last uploaded.
- `./companion app` runs the menu-bar app from the terminal.

### Contributing

`go test ./...` runs the tests. To try the Companion against a server of your own, give its address to `link` (or `app`) with `--server`; it then uploads there.

To upload to your server and to PriceWatch Forever at once, keep the app linked to PriceWatch Forever and run a second copy with settings of its own (on macOS, its own `HOME`) and `--upload-only`:

```sh
mkdir -p ~/.companion-test
HOME=~/.companion-test ./companion link --server https://your.server
HOME=~/.companion-test ./companion run --upload-only
```

The second copy never writes the PriceWatch_Companion addon: the addon throws away what the app has uploaded, and only the app can say what that is. So the second copy can miss records the app has uploaded, never the other way round.

`go generate ./internal/icon` draws the icons again.

## License

[MIT](LICENSE)

---

PriceWatch Forever is a fan project, not affiliated with or endorsed by Blizzard Entertainment. World of Warcraft is a trademark of Blizzard Entertainment, Inc.
