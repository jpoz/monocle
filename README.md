# rsvp

A terminal speed reader using RSVP (Rapid Serial Visual Presentation) with
Spritz-style ORP alignment. Words flash one at a time with their optimal
recognition point — the letter your eye actually fixates on, roughly a third
of the way into the word — pinned to the center of the screen and highlighted
red, so your eyes never move. The surrounding paragraph text is shown greyed
out around the focal word for context.

## Install

```sh
go install github.com/jpoz/rsvp@latest
```

Or from this directory:

```sh
go build -o rsvp .
```

## Usage

```sh
rsvp README.md          # default 350 wpm
rsvp -w 500 notes.md    # 500 wpm
```

Markdown files (`.md`, `.markdown`, `.mdx`) have their formatting stripped
(headings, emphasis, links, list markers, code fences); other files are read
as plain text. Paragraphs are split on blank lines.

## Controls

| Key       | Action                                            |
| --------- | ------------------------------------------------- |
| `space`   | play / pause                                      |
| `←` / `→` | back / forward one word                           |
| `↑`       | restart paragraph (again: previous paragraph)     |
| `↓`       | next paragraph                                    |
| `+` / `-` | speed up / slow down by 25 wpm                    |
| `q`       | quit                                              |

`h j k l` work as vim-flavored aliases for the arrows.

Playback lingers a little longer on punctuation, long words, and paragraph
ends, which helps comprehension at higher speeds.
