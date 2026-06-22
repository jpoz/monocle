# monocle

A focused document reader for the terminal — examine one thing at a time.
The current paragraph is shown bright with your position highlighted;
everything above and below is dimmed. You move the focus word-by-word and
line-by-line at your own pace — your position stays vertically centered, so
your eyes never travel.

(It started life as an automatic RSVP/Spritz-style speed reader, but the
user-controlled focus view turned out to be the good part.)

## Install

```sh
make install        # builds and installs to ~/bin
```

Or `go build -o monocle .` and put the binary wherever you like.

### Better read-aloud voices

[Read aloud](#read-aloud) (macOS only) sounds far better with Apple's natural
voices, which aren't installed by default — macOS ships only low-quality
"compact" voices, so out of the box the choices are robotic and nearly
identical. The good voices are free, but there's **no supported command-line
installer**: they're on-demand system assets that only the Spoken Content UI can
fetch. You can at least jump straight to the right pane:

```sh
open "x-apple.systempreferences:com.apple.Accessibility-Settings.extension"
```

Then **Spoken Content → System Voice → Manage Voices…**, expand **English (US)**,
and check a few good ones:

- **Ava (Premium)** — Siri-quality, the best of the bunch
- **Zoe (Premium)** — Siri-quality
- **Evan (Enhanced)**, **Nathan (Enhanced)**, **Joelle (Enhanced)**, **Tom (Enhanced)**
- **Samantha (Enhanced)** — a big step up from the default compact Samantha

They download in the background (Premium are ~100–200 MB each). Once installed
they show up in monocle automatically and, being higher quality, lead the `v`
voice cycle — no restart needed. (Siri's own voices aren't available to
third-party apps, so they can't appear.)

## Usage

```sh
monocle README.md
```

Markdown files (`.md`, `.markdown`, `.mdx`) are parsed structurally and
rendered with their structure intact:

- **headings** become section markers (shown in the status bar, jumpable
  with `n`/`p`)
- **tables** render as an aligned grid with `│` column separators and an
  underlined header row; up/down moves between cells. Tables wider than the
  text column shrink their widest columns and wrap cell text to fit
- **code blocks** keep their exact lines and indentation (never wrapped)
  and render tinted
- **lists** get `•` bullets (or their number) with hanging indent, one item
  per paragraph; **blockquotes** get a `▎` bar
- **inline styles** render: **bold**, *italic*, `code`, ~~strikethrough~~,
  and links (underlined, URL dropped)

Other files are read as plain text with paragraphs split on blank lines.

## HTML files (browser review)

HTML files (`.html`, `.htm`) don't open in the terminal reader — monocle spins
up a local web server, opens the file in your browser, and overlays a
commenting layer on top of it:

```sh
monocle interviews/notification.html
```

The document renders unchanged inside an iframe (its own CSS, images, and
relative links all work — assets are served from the file's directory), with a
comment sidebar alongside it. To leave a note, **select any text** and click the
**💬 Comment** button that appears; type your note and press `⌘`/`Ctrl`+`Enter`
(or *Comment*) to save. Saved comments highlight their passage in the document
and show as cards in the sidebar — click either to jump to the other. Cards have
*Edit* and *Delete*; saving an empty edit deletes the note.

The toolbar's **Export** button copies every comment to your clipboard as the
same Markdown the terminal reader produces (see [Comments](#comments)), and
**Copy path** copies the file's path. The server runs on `127.0.0.1` on a random
port; press `Ctrl-C` in the terminal to stop it.

Comments are anchored to the selected text (the quote plus a little surrounding
context), so they survive small edits and re-find the right passage even when
the same words appear more than once. They persist in
`~/.config/monocle/html-comments.json`, keyed by the document's path.

## Controls

| Key                 | Action                                          |
| ------------------- | ----------------------------------------------- |
| `←`/`→` `h`/`l`     | previous / next word                            |
| `↑`/`↓` `k`/`j`     | previous / next visual line (column-preserving) |
| `⇧`+move / `H``J``K``L` | extend a selection                          |
| `c`                 | comment on the selection (or current line)      |
| `x`                 | export all comments to the clipboard            |
| `p`                 | copy the file's path (relative to the git root) |
| `{` / `}`           | paragraph start / next paragraph                |
| `n` / `b`           | next / previous section (markdown headings)     |
| `r`                 | read aloud from the focal word (any key stops)  |
| `v` / `V`           | next / previous voice while reading             |
| `g` / `G`           | top / end of document                           |
| `+` / `-`           | wider / narrower text column                    |
| `s`                 | style picker                                    |
| `t`                 | theme picker                                    |
| `q`                 | quit                                            |

Up/down move between *wrapped* lines, like a text editor: the focus lands on
the word nearest your current column, and crosses paragraph boundaries
seamlessly.

## Read aloud

Press `r` to read aloud from the focal word to the end of its section (the
heading-delimited block the cursor is in). The word highlight tracks the
speech, word by word, as it plays. Pressing any key stops both the audio and
the highlight. This is macOS-only.

Press `v` (or `V` for the previous one) while reading to switch voice — it
cycles through the installed voices for your locale and restarts from the
current word. The choice persists across runs. (Voice switching needs the Swift
helper below; the `say` fallback ignores it.)

Out of the box the cycle is short and robotic — macOS only ships low-quality
"compact" voices, and the near-identical MacinTalk novelty voices are hidden.
Install Apple's natural voices for a dramatic upgrade; see
[Better read-aloud voices](#better-read-aloud-voices).

To keep the highlight in exact step with the audio, monocle uses a small Swift
helper built on `AVSpeechSynthesizer`, whose word-boundary callbacks report the
real word being spoken (rather than estimating per-word timing, which drifts on
numbers, abbreviations, and the synthesizer's own pauses). It is compiled from
an embedded source on first use and cached, so it needs the Swift toolchain
(`swiftc`, included with the Xcode Command Line Tools). Without it, monocle
falls back to the `say` command with an estimated, less precise highlight.

## Styles

Press `s` to open the style picker (`j`/`k` select, `space` toggles,
`esc` closes):

- **Word highlight** — reverse-video highlight on the focal word
- **Line highlight** — a background bar across the current line
- **Dim other paragraphs** — non-current paragraphs render dimmed

## Themes

Press `t` to open the theme picker (`j`/`k` to move, which previews the theme
live; `esc`/`enter` closes). The chosen theme persists across runs. Bundled
themes:

- **Default** (monocle's original look)
- **Nord**
- **Dracula**
- **Gruvbox Dark**
- **Solarized Dark** / **Solarized Light**
- **Tokyo Night**
- **Catppuccin Mocha**
- **One Dark**
- **Monokai**

Colors are truecolor where the terminal supports it, degrading gracefully to
the 256-color palette otherwise.

## Comments

Leave notes on passages as you read:

- Hold `⇧` and move (or press capital `H`/`J`/`K`/`L`) to select a span; the
  selection highlights as you extend it. `esc` clears it.
- Press `c` to comment. With a selection active the note attaches to the
  selected span; otherwise it attaches to the current line. Pressing `c` while
  the focus sits inside an existing comment reopens it for editing.
- A small multi-line editor opens in the right margin, beside the text rather
  than over it: type your note, `ctrl+s` to save, `esc` to cancel. Saving an
  empty body deletes the comment.

Saved comments live in a right-hand margin, each card aligned to the lines it
annotates (like a document's comment sidebar); the focused note brightens.
Commented lines also get a `▌` marker in the left gutter. The margin needs a
wide enough terminal — on narrow ones the editor falls back to a centered box
and the note shows in the status bar instead.

Press `x` to copy every comment for the document to the clipboard as Markdown —
each note quotes its passage and names its section, a format an LLM can map
straight back to the source:

```markdown
# Comments on README.md

## 1. Comments
> Hold ⇧ and move (or press capital H/J/K/L) to select a span

clarify that esc also exits the reader when nothing is selected
```

## State

Reading position, style choices, theme, and column width persist across runs
in `~/.config/monocle/state.json` (honoring `$XDG_CONFIG_HOME`). Reopening a
file resumes where you left off; finished documents — or ones whose content
changed since — start over from the beginning.

Comments live alongside it in `~/.config/monocle/comments.json`, keyed by the
document's path.
