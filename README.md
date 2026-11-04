# PriceWatch Companion

A desktop companion for the PriceWatch addon in World of Warcraft: Forever. Link it to your [PriceWatch Forever](https://pricewatchforever.com) account and your play sessions show up on the website: where you went, what you looted and what it was worth.

It's free, with nothing behind a payment.

**Status:** not released yet.

## What it touches on your computer

The Companion is open source so you can check exactly what it does:

- **It reads one file:** the PriceWatch addon's saved data, `PriceWatch.lua`, in your game folder under `WTF/Account/<account>/SavedVariables/`. It never runs that file and never changes it.
- **It writes one small addon of its own:** `PriceWatch_Companion`, which tells the PriceWatch addon that the Companion is linked and what it has already sent, so the addon can clear out what's been sent.
- **It sends your sessions to your PriceWatch Forever account only.** They're private to you and kept until you delete them.
- **It never talks to Blizzard** and holds no Blizzard keys.

## License

[MIT](LICENSE)

---

PriceWatch Forever is a fan project, not affiliated with or endorsed by Blizzard Entertainment. World of Warcraft is a trademark of Blizzard Entertainment, Inc.
