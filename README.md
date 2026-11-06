# PriceWatch Companion

A desktop companion for the PriceWatch addon in World of Warcraft: Forever. Link it to your [PriceWatch Forever](https://pricewatchforever.com) account and your play sessions show up on the website: where you went, what you looted and what it was worth.

It's free, with nothing behind a payment.

**Status:** not released yet. You can build it from source and run it on macOS.

## What it touches on your computer

The Companion is open source so you can check exactly what it does:

- **It reads one file per WoW account:** the PriceWatch addon's saved data, `PriceWatch.lua`, in your game folder under `WTF/Account/<account>/SavedVariables/`. It never runs that file and never changes it.
- **It writes one small addon of its own:** `PriceWatch_Companion`, in your game folder under `Interface/AddOns/`, which tells the PriceWatch addon that the Companion is linked and what it has already sent, so the addon can clear out what's been sent. It changes no other game file.
- **It keeps its link to your account in its own settings file,** outside the game folder, readable only by you: on macOS, `~/Library/Application Support/PriceWatch Companion/config.json`.
- **It sends your sessions to your PriceWatch Forever account only.** They're private to you and kept until you delete them. When linking, it also sends your computer's name, so the website can show you which computer is linked, and with each request its own version number.
- **It never talks to Blizzard** and holds no Blizzard keys.

## Build and run

You need [Go](https://go.dev/dl/) 1.24 or later and a terminal. For now it runs on macOS.

```sh
git clone https://github.com/pricewatch-forever/companion.git
cd companion
go build
```

That makes a program called `companion` in the folder.

**Link it to your account, once:**

```sh
./companion link
```

It shows a code and opens the PriceWatch Forever website in your browser. Sign in there and confirm the code.

**Run it while you play:**

```sh
./companion run
```

It uploads what PriceWatch has recorded so far, then again each time the game saves it (when you log out or `/reload`). Leave it running in its terminal window; Ctrl-C stops it.

The first time, it adds its own small addon to the game. The game only finds a new addon when it starts, so restart the game then: a `/reload` isn't enough.

If your game isn't in `/Applications/World of Warcraft/_forever_`, give its folder with `--game`:

```sh
./companion run --game "/path/to/World of Warcraft/_forever_"
```

`./companion status` says whether it's linked and when it last uploaded. If you unlink the computer on the website, the Companion stops uploading and says so; `./companion link` links it again.

### Contributing

`go test ./...` runs the tests. To try the Companion against a server of your own, give its address to `link` with `--server`; `run` then uploads there.

## License

[MIT](LICENSE)

---

PriceWatch Forever is a fan project, not affiliated with or endorsed by Blizzard Entertainment. World of Warcraft is a trademark of Blizzard Entertainment, Inc.
