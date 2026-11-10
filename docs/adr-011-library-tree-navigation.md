# ADR-011: The library tree is for choosing, the playlist is for playing

**Status:** Accepted 2026-11-09. Cue sheets as leaves and the playlist-source highlight were added
2026-11-10 in 1.0.8 (written down 2026-11-10).
**Overcome:** fully, nothing open.

## Context

The library is a folder tree of FLAC files and cue sheets. The UI needs a way to pick what to
listen to and a place where playback happens. The owner's reference points are foobar2000's
album list and the project view in JetBrains IDEs.

## Options and what was rejected

**What the UI is built around**

- A central list of all tracks under the selected folder. Rejected 2026-11-09: large folders
  produced thousands of rows to scroll.
- **The folder tree plus a playlist.** Chosen.

**Starting playback**

- Double-click in the tree starts playback. Rejected 2026-11-09: the tree is for navigation, the
  playlist is what plays.
- Double-click on a playlist track. Rejected 2026-11-09 in favour of an explicit ▶ button.

**Choosing a device**

- A dropdown. Rejected 2026-11-09 in favour of a list with a Use button.

**Cue sheets in the tree**

- A cue sheet expands into its tracks. Dropped 2026-11-10: the playlist shows the same tracks, so
  the tree only repeated them.
- **A cue sheet is a leaf, like a track file.** Chosen.

**What the tree highlights**

- The playing track. Replaced 2026-11-10: with cue sheets as leaves it had nothing to point at.
- **The node the playlist was built from**, as foobar2000's selection does. Chosen.

## Decision

- Folders with nothing playable inside are hidden.
- The → button on a tree row builds the playlist from that node (`PUT /api/queue {ids, source}`).
  Playback is not touched.
- Only the playlist plays: its ▶ button or Play in the top bar.
- The highlighted source is kept with the playlist (`playlist.source` in `config.json`), so it
  survives a restart. A playlist built from search results has no source, and nothing is
  highlighted.
- The crosshair button (Alt+F1), like "Select Opened File" in JetBrains IDEs, shows the source in
  the tree. For a playlist built from search results it shows the cue sheet or file of the playing
  track instead.
- The tree does not read cue sheets itself. They are parsed during the library scan, because the
  playlist, search and track lengths need their tracks.

## Consequences

- A single track of a cue sheet can no longer become a playlist from the tree. It can still be
  played from the playlist.
- The FLAC image stays visible next to its cue sheet, as in foobar2000.
