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

## Usage

```sh
monocle README.md
```

Markdown files (`.md`, `.markdown`, `.mdx`) are parsed structurally and
rendered with their structure intact:

- **headings** become section markers (shown in the status bar, jumpable
  with `n`/`p`)
- **tables** render as an aligned grid with `│` column separators and an
  underlined header row; up/down moves between cells
- **code blocks** keep their exact lines and indentation (never wrapped)
  and render tinted
- **lists** get `•` bullets (or their number) with hanging indent, one item
  per paragraph; **blockquotes** get a `▎` bar
- **inline styles** render: **bold**, *italic*, `code`, ~~strikethrough~~,
  and links (underlined, URL dropped)

Other files are read as plain text with paragraphs split on blank lines.

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
| `g` / `G`           | top / end of document                           |
| `+` / `-`           | wider / narrower text column                    |
| `s`                 | style picker                                    |
| `t`                 | theme picker                                    |
| `q`                 | quit                                            |

Up/down move between *wrapped* lines, like a text editor: the focus lands on
the word nearest your current column, and crosses paragraph boundaries
seamlessly.

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
