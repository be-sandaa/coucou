<p align="center">
  <img src="docs/wiki/images/colette-wide.jpg" alt="Colette, with cherry-red streaks in her dark hair, leaning back against her desk in her Paris bedroom beside a dress form, a bed heaped with costumes on the left" width="720">
</p>

<p align="center"><b>It waits in the wings. Then, from nowhere: <i>coucou!</i></b></p>

<p align="center">
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26"></a>
  <a href="https://github.com/be-sandaa/coucou/releases/latest"><img src="https://img.shields.io/badge/release-v0.5.0-E4572E" alt="release v0.5.0"></a>
  <a href="https://github.com/be-sandaa/coucou/pkgs/container/coucou"><img src="https://img.shields.io/badge/image-ghcr.io%2Fbe-sandaa%2Fcoucou-2496ED?logo=docker&logoColor=white" alt="image ghcr.io/be-sandaa/coucou"></a>
  <a href="https://github.com/be-sandaa/coucou/wiki/Architecture#memory"><img src="https://img.shields.io/badge/RSS%20target-25%20MB%20%40%201.6k%20guilds-4E5058" alt="RSS target 25 MB at 1.6k guilds"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-F2B705" alt="MIT license"></a>
</p>

*Coucou!* is French for "peekaboo", or a cheeky "hi there". That is the entire product.

coucou runs Discord bots that slip into a busy voice channel, play one short sound, and leave
before anyone can react. Then everybody blames each other.

## 🎬 A visit, as seen from the channel

```text
🔊 General ·  👤 alex   👤 sam   👤 robin

   🎲  the 5-minute roll comes up
   🧵  Colette takes a costume off the rail… et hop !
   🍷  Victoria joined
   ⋯   (a long, suspicious silence)
   🍷  ▶ Ahem.
   🍷  Victoria left

   💬  sam: alex
   💬  alex: that was NOT me
```

## ✨ What it does

| | |
|---|---|
| 🎲 **Drop-ins** | Every 5 minutes it rolls for each server, and on a win picks the busiest voice channel with a human in it. |
| 🤫 **Suspense** | Sits in silence for up to 20 seconds first, if a server wants it to. |
| 🃏 **Fake-outs** | Sometimes joins, says nothing, and leaves. |
| 🔁 **Encores** | Comes back for a second sound, and a sound can name what follows it. |
| ⛓️ **Chains** | *Knock knock… who's there… rimshot*, all in one visit. |
| ✨ **Rares** | A tenth as often, never on request, and collected in `/stats`. |
| 🔞 **18+ sounds** | Where Discord has age-restricted the server or channel, or wherever a server's admins choose with `/nsfw`. |
| 📊 **Stats** | Charts, heatmaps and leaderboards of who suffers most. |
| 🙈 **Escape hatches** | `/optout` for a person, `/quiet` for a server, both on a schedule if you like. |

## 💋 Meet Colette

Colette is the bot run from here: a costume student who works nights in the wardrobe of a small
Pigalle cabaret, and tries every costume on after closing, "for the fit". Your server is her fitting
room: she takes someone else's costume off the rail and drops into your voice channel as them, or
pops out from behind the curtain as herself with a sing-song "coucou!".

<p align="center">💋 🌺 🖤 📚 💜 🍷</p>

Each server picks who she dresses up as with `/character`: a Latina who purrs in Spanglish, a goth
who says something mean she definitely means nicely, a shy librarian who reads you her spicy novel
by accident, a rich, strict stepmom who runs her house like a boardroom… → [all of them](https://github.com/be-sandaa/coucou/wiki/Characters). Want more than one
in the same server? Each also runs as a bot of its own, and a couple, like Gus 💨, only ever run as
themselves → [standalone bots](https://github.com/be-sandaa/coucou/wiki/Standalone-bots).

**[Add Colette to your server](https://discord.com/oauth2/authorize?client_id=1557651122460557384&scope=bot+applications.commands&permissions=70274048)**, or any character on her own from [her page](https://github.com/be-sandaa/coucou/wiki/Characters).

A few things she will not tell you:

- Her graduate collection is meant to be her own. Every sketch turns into somebody else's costume.
- She counts "un, deux, trois" under her breath before anything brave, even a voice channel.
- Whoever she is dressed as, the cherry-red streaks and the red soles stay. That's how you catch her.

## 🎭 Bring your own bot

The project, coucou, isn't a character; Colette is just the one run from here. Give it one and you have a new bot: one binary, your character, your
Discord app, your sounds.

1. **Write the character.** A `profile.toml`: a name, an emoji, a status, lore for `/about`, and how
   a server starts out. Its avatar is an image beside it, or the avatar of its own bot, GIFs and
   all. → [Profile](https://github.com/be-sandaa/coucou/wiki/Profile)
2. **Make the sounds.** `scripts/sound ~/Downloads/noises/` encodes a folder of clips and names them
   for you. → [Sounds](https://github.com/be-sandaa/coucou/wiki/Sounds)
3. **Run it.** A release binary, the container image, Compose or the Helm chart, with a token from
   your own Discord application. → [Installation](https://github.com/be-sandaa/coucou/wiki/Installation)
4. **Get it listed.** Open a [**List my bot**](https://github.com/be-sandaa/coucou/issues/new?template=list_my_bot.yml)
   issue and it joins the table below.

To be listed, a bot runs on its own Discord application, with sounds it has the right to use, and
follows Discord's [Terms of Service](https://discord.com/terms) and
[Developer Policy](https://support-dev.discord.com/hc/en-us/articles/8563934450327-Discord-Developer-Policy).

### 🏆 Community bots

| Bot | What it does | Maintainer | |
|---|---|---|---|
| 💨 [Gus](https://github.com/be-sandaa/coucou/wiki/Gus) | Wanders in, farts, burps, crunches chips into the mic, and leaves you all blaming each other. | [@be-sandaa](https://github.com/be-sandaa) | [Invite](https://discord.com/oauth2/authorize?client_id=1286266858642870333&scope=bot+applications.commands&permissions=70274048) |
| 😳 [Moan](https://github.com/be-sandaa/coucou/wiki/Moan) | Lets out one very unfortunate moan, and vanishes before anyone can explain it. | [@be-sandaa](https://github.com/be-sandaa) | [Invite](https://discord.com/oauth2/authorize?client_id=941362698472546404&scope=bot+applications.commands&permissions=70274048) |
| *yours here* | | | |

## 🚀 Quick start

```sh
docker run --rm \
  -e DISCORD_BOT_TOKEN=... \
  -e DATABASE_URL=sqlite:///var/lib/coucou/coucou.db \
  -v coucou-data:/var/lib/coucou \
  -v "$PWD/profile:/var/lib/coucou/profile:ro" \
  ghcr.io/be-sandaa/coucou:0.5.0
```

That needs a Discord application and a profile folder first; [Running the bot](https://github.com/be-sandaa/coucou/wiki/Running-the-bot)
walks through both in three steps.

## 📚 Documentation

| | Page | For |
|---|---|---|
| 📦 | [Installation](https://github.com/be-sandaa/coucou/wiki/Installation) | binary, systemd, container, Compose, Helm |
| ▶️ | [Running the bot](https://github.com/be-sandaa/coucou/wiki/Running-the-bot) | the Discord app, sounds, first start, "is it working?" |
| ⚙️ | [Configuration](https://github.com/be-sandaa/coucou/wiki/Configuration) | every key in `config.toml` |
| 🪞 | [Characters](https://github.com/be-sandaa/coucou/wiki/Characters) | who Colette can dress up as, and the standalone bots |
| 🎭 | [Profile](https://github.com/be-sandaa/coucou/wiki/Profile) | the character, its status, defaults, chains and links |
| 🔊 | [Sounds](https://github.com/be-sandaa/coucou/wiki/Sounds) | encoding, naming, tags, sound emojis |
| 💬 | [Commands](https://github.com/be-sandaa/coucou/wiki/Commands) | every slash command and who may run it |
| 🛠️ | [Development](https://github.com/be-sandaa/coucou/wiki/Development) | building, testing, the image |
| 🧭 | [Architecture](https://github.com/be-sandaa/coucou/wiki/Architecture) | how a play happens, the database, the bus, memory |

## 🛠️ Hacking on it

```sh
make build   # vet + test, then a static binary in bin/coucou
make dev     # run the checkout against one test server
make help    # everything else
```

Go 1.26, no CGO, both SQLite and PostgreSQL in one binary. Start with
[Architecture](https://github.com/be-sandaa/coucou/wiki/Architecture), and [CONTRIBUTING.md](CONTRIBUTING.md) before a PR.

## 📄 License

[MIT](LICENSE). The bots run from this repository follow its [Terms](TERMS.md) and
[Privacy Policy](PRIVACY.md).
