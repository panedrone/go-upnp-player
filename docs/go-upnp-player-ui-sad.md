# go-upnp-player — UI

The page (`web/`) and what it shows: layout, controls, look. The backend under it:
[go-upnp-player-sad.md](go-upnp-player-sad.md). Why it is built this way: the ADRs,
[go-upnp-player-adr.md](go-upnp-player-adr.md).

The web page below is the player's UI. A Qt window, `cmd/go-upnp-player-qt`, is being built beside
it from the same layout and behaviour ([ADR-027](adr-027-qt-window-with-miqt.md)). So far it has the
search box over the library tree, with refresh (rescan) and Collapse All by it (the results in its place while it has text: → makes a playlist
of one, a button of them all, a click shows the track in the tree), the tree with → to make a
playlist, the playlist grouped by album as on the page (a header with the cover, the title, artist and
year and the total time; a click on a row plays it, the playing row bold with the play arrow left of its number, its i shows the
properties), the Properties pane under it (following the playing track until an i is clicked),
the crosshair (Alt+F1) that shows the playlist's source in the tree, once by itself on start,
the transport and seek slider, the devices (a click uses one) with the volume, the Equalizer card
(presets and `.feq` files from `services`), the Library folders card, and the status bar with the
busy message, the error and the version. The theme (dark by default, the page's Bootstrap colours
on Qt's Fusion style), the window's place and size, the splitters, the folded cards and the tree's open nodes
(the roots open on the first run) are kept in the config's `ui` section. Check for updates (⭳ by the volume) asks GitHub on the click and offers
the release page in the status bar: the Qt copy has no setup yet, and `go install` would build the
web player.

- **Check for updates** (the cloud button by the version in the status bar, [ADR-019](adr-019-check-for-updates-on-demand.md)):
  the player asks GitHub only on this click. A newer version is offered in the status bar, with a
  ×, the way this copy updates: the setup copy downloads the new setup, a `go install` copy
  rebuilds itself and restarts, the zip opens the release page. Pages open in the system browser
  through `POST /api/update/open`.
- **Status bar.** The busy message and the error start at the left, the update offer stays at the
  right by the version. No text is cut with an ellipsis: a long one wraps and the bar grows. The
  error has a × that hides it until it changes; every × sits centred beside its text.
- **State.** The page reads `GET /api/events` (Server-Sent Events): the state now, then one event
  per change, the same JSON as `GET /api/state`. The backend wakes the stream after every poll of
  the renderer (once a second) and after every command, and sends a snapshot only if it differs
  from the last; a keepalive comment every 15 s. After every command the page also asks
  `GET /api/state` once ([ADR-026](adr-026-state-as-server-sent-events.md)). A changed
  `library.scans` reloads the tree and the playlist. The backend's state is shown as it is: a seek
  or volume slider shows the value being dragged, and from the first state read after the command
  answered, the backend's.
- **Layout.** One page, no modal dialogs:
  - three columns with splitters (`react-resizable-panels`; they stack on screens ≤ 900 px);
  - Library tree, playlist, and on the right the Output, Equalizer, DSD and Library folders cards;
  - under the playlist, behind a splitter of its own, the Properties card.
- **Library tree** ([ADR-011](adr-011-library-tree-navigation.md)):
  - folders, cue sheets and track files (FLAC, WAV, DSF, DFF), names only (lengths are in the
    playlist); cue sheets and files are leaves;
  - → on a row builds the playlist from the node without touching playback;
  - a folder can be starred, to mark the releases that sound better: a set star stays on the row
    in yellow, on hover the star is a button with the current value left of →, a click flips it
    (`PUT /api/starred`; `GET /api/tree` gives `starred` on folders). A renamed or moved folder
    loses its star. The Qt player does not show stars;
  - the node the playlist was built from is highlighted.
- **Search** (the box in the library header) shows matching tracks in place of the tree: → on a
  result makes a playlist of it, "Make a playlist of all" of them all, and a click on a result
  shows it in the tree, leaving the search, as the crosshair does for the playlist.
- **Library header:** the search box in place of a title, then the buttons:
  - refresh: rescan the library folders; the tree reloads in place;
  - Collapse All: fold everything below the library roots.
- **Controls** sit in the column they belong to. The playlist's header has no title (the playlist
  is what they play): on the left the crosshair (Alt+F1), which shows the playlist's source in the
  tree (the page does it once by itself when it opens), then the seek slider, and the transport on
  the right; volume, theme and Quit in a card above Output
  (volume is the selected device's). What plays is shown in the playlist: its row is bold and
  marked ▶ left of the track number. A click anywhere on a row plays it; under the pointer its title
  is underlined. The i button right after the title, shown on row hover like the tree's buttons, shows the
  track's properties and leaves playback alone. Album headers show
  the total time.
- **The years** in the playlist's album headers (page and Qt): the album's own year, then the
  release's after a slash when they differ, "1984 / 2015" for a 2015 reissue
  (`services.AlbumYears`). The album's year comes from the library's folders, as the tree shows
  it (`services.AlbumYear`: the nearest folder named "1984 …", "(1987) …", "… - 2022 - …" or "… [2020]");
  the release's is the DATE tag's, which is also the fallback.
- **Properties** of the playlist track whose i was clicked last (the playing track until then), as
  foobar2000's Properties without Location: Metadata (artist, title, album, date, genre, track
  number, comment) and General (duration with samples, rate, channels, bits, bitrate, codec,
  encoding, tool, embedded cuesheet, audio MD5). `GET /api/track/:id/properties` reads the FLAC
  header again on each request; vendor, MD5 and comment are not kept in the index. The bitrate of
  a cue track is its file's average. A DSD track shows its own rate, 1 bit and e.g. "DSD128
  (DSF)", and in Encoding the PCM it plays as. A WAV track shows "PCM (WAV)"; the Output line
  names the format of the playing file (FLAC, WAV, cue → WAV, DSD… → PCM).
- **DSD card** ([ADR-028](adr-028-dsd-converted-to-pcm-in-the-player.md)): the PCM rate
  (44.1 / 48 / 88.2 / 96 / 176.4 / 192 kHz as in foobar2000, 176.4 by default,
  [ADR-029](adr-029-dsd-every-rate-up-to-192.md)) and the gain (0 / +3 / +6 dB, +6 by default, as
  foobar2000) as two selects on one line; a change saves at once (`GET`/`PUT /api/dsd`) and restarts a
  playing DSD track where it is (`player.SetDSD`; a new rate rescans the library first); a spinner
  right of the selects while that is under way (`dsdApplying` in the state).
  Foldable like the other cards.
- **Playlist and devices.** Only the playlist plays (a click on its row, or Play in its header). Devices are a
  list in the Output card with a Use button; Refresh and the Scan switch in its header. The selected device's row carries its transport badge after its name
  (a lively green for PLAYING, light gray for quiet states, PAUSED on dark amber with white text for PAUSED_PLAYBACK); the
  Output header shows a badge only when the player itself is offline. The Output title line
  also carries what is streamed and from where (local IP, rate, bits, channels, WAV or FLAC, or
  e.g. "DSD128 → PCM"),
  parts separated by bold dots. Row buttons appear on hover and are always visible on
  touch screens.
- **Waiting for the renderer.** A spinner shows after the title of the row being started, and the
  Use button turns into one, while their request runs. Those requests answer only after the renderer has really switched.
- **Look.** Stock Bootstrap, gray instead of blue:
  - the active row is a gray band with bold text, other rows are `text-body-secondary`;
  - buttons are `secondary` or `outline-secondary`;
  - custom CSS is limited to the slider thumbs and button sizes; the ▶ mark of the playing row is
    the Play button's SVG arrow, not a font symbol (Windows drew ▶ as a blue emoji);
  - button icons are drawn SVG of one size and stroke; a button without a label is a 24 px
    square that never shrinks (Quit included). Font symbols came out in different sizes and off
    centre;
  - equalizer presets, Load… and Save… are quiet gray words, not framed buttons: the equalizer
    is a side tool and must not pull the eye;
  - album art in the playlist is 64 px;
  - everything that is clicked or dragged shows the hand cursor: buttons, rows, switches and their
    labels, sliders, selects. A new control gets it too; a disabled one does not.
- **Icon.** `web/public/favicon.svg` is the source (a streamer box with a play badge);
  `web/public/favicon-32.png`, `assets/icon-256.png` and `assets/icon.ico` are rendered from it.
  The `.ico` goes beside the exe in the zip and the setups, and a copy in
  `cmd/go-upnp-player/window` gives the window its icon. The web player's exe has no embedded icon
  (that needs a `.syso` resource); the Qt exe has one (its `icon.rc`).
