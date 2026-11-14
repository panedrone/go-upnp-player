import { ChangeEvent, useCallback, useEffect, useRef, useState } from 'react'
import { Badge, Button, ButtonGroup, Card, Form, InputGroup, ListGroup, Spinner } from 'react-bootstrap'
import { api, fmtTime, PlayerState, Track, TreeNode, UpdateOffer } from './api'
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels'
import { RefreshIcon, ToPlaylistIcon, UpdateIcon } from './icons'
import Tree, { RevealRequest } from './Tree'
import Playlist from './Playlist'
import TrackProperties from './TrackProperties'
import Equalizer from './Equalizer'
import Settings from './Settings'


// Badge colours for the transport state. "playing" and "idle" are our own (styles.css):
// Bootstrap's success green and secondary gray looked the same at a glance, so playing
// is a livelier green and every quiet state a lighter gray.
const transportVariant: Record<string, string> = {
  PLAYING: 'playing',
  PAUSED_PLAYBACK: 'warning',
  OFFLINE: 'danger',
  STALLED: 'danger',
  'BACKEND OFFLINE': 'danger',
}

// Short badge labels for the UPnP transport states that are too long for a device row.
const transportLabel: Record<string, string> = {
  PAUSED_PLAYBACK: 'PAUSED',
  NO_MEDIA_PRESENT: 'NO MEDIA',
}

export default function App() {
  const [state, setState] = useState<PlayerState | null>(null)
  const [connected, setConnected] = useState(false)
  const [queue, setQueue] = useState<Track[]>([])
  const [filter, setFilter] = useState('')
  const [searchHits, setSearchHits] = useState<Track[] | null>(null)
  const [actionErr, setActionErr] = useState('')
  const [treeKey, setTreeKey] = useState(0)
  const [quitArmed, setQuitArmed] = useState(false)
  const [quitting, setQuitting] = useState(false)

  // light/dark theme: Bootstrap's data-bs-theme on <html>; a per-browser
  // convenience kept in localStorage (index.html applies it before the first paint)
  const [theme, setTheme] = useState(() => document.documentElement.getAttribute('data-bs-theme') === 'light' ? 'light' : 'dark')
  useEffect(() => {
    document.documentElement.setAttribute('data-bs-theme', theme)
    try {
      localStorage.setItem('go-upnp-player.theme', theme)
    } catch {
      // no storage: the theme just is not remembered
    }
  }, [theme])

  // "Check for updates": asked only on a click, the answer shown in the status bar until
  // the user closes it (the × or a click on the message): it does not run away by itself
  type UpdateNote =
    | { kind: 'checking' }
    | { kind: 'offer'; offer: UpdateOffer }
    | { kind: 'latest'; version: string }
    | { kind: 'installing'; version: string }
    | { kind: 'installed'; version: string }
    | { kind: 'error'; message: string }
  const [updateNote, setUpdateNote] = useState<UpdateNote | null>(null)
  const checkUpdate = () => {
    setUpdateNote({ kind: 'checking' })
    api
      .checkUpdate()
      .then((o) => setUpdateNote(o.newer ? { kind: 'offer', offer: o } : { kind: 'latest', version: o.current }))
      .catch((e) => setUpdateNote({ kind: 'error', message: e instanceof Error ? e.message : String(e) }))
  }
  const installUpdate = (version: string) => {
    setUpdateNote({ kind: 'installing', version })
    // the page reloads itself once the new build answers the poll; if it has not after
    // 30 s, the restart did not take, and the user is told to restart by hand rather than
    // left with "Updating…" for ever
    api
      .installUpdate()
      .then(() => setTimeout(() => setUpdateNote((n) => (n?.kind === 'installing' ? { kind: 'installed', version } : n)), 30000))
      .catch((e) => setUpdateNote({ kind: 'error', message: e instanceof Error ? e.message : String(e) }))
  }

  // an "armed" Quit button disarms itself after 3 s
  useEffect(() => {
    if (!quitArmed) return
    const t = setTimeout(() => setQuitArmed(false), 3000)
    return () => clearTimeout(t)
  }, [quitArmed])
  const narrow = useNarrow()
  const queueVersion = useRef(-1)
  const seenBuild = useRef('')
  const seenScans = useRef(-1)

  // player state: the page asks /api/state once a second (the backend polls the
  // renderer at the same rate) and right after every command, so a click shows its
  // result at once. No long-lived channel: every reading is a plain request.
  const refresh = useRef<() => void>(() => {})
  const stateAskedAt = useRef(0) // when the request behind the current state was sent
  useEffect(() => {
    let timer = 0
    let stopped = false
    let busy = false // one request at a time: a refresh during one asks for another after it
    let again = false
    const poll = async () => {
      if (busy) {
        again = true
        return
      }
      busy = true
      clearTimeout(timer)
      try {
        const asked = Date.now()
        const st = await api.state()
        if (stopped) return
        setConnected(true)
        // the player under the page was replaced by another build (an update):
        // reload, otherwise the old UI keeps its stale state, e.g. tree folders
        // expanded before the update never refetch (2026-11-09)
        if (st.build) {
          if (seenBuild.current && seenBuild.current !== st.build) {
            window.location.reload()
            return
          }
          seenBuild.current = st.build
        }
        stateAskedAt.current = asked
        setState(st)
      } catch {
        if (!stopped) setConnected(false)
      }
      busy = false
      if (stopped) return
      if (again) {
        again = false
        void poll()
      } else {
        timer = window.setTimeout(poll, 1000)
      }
    }
    refresh.current = () => void poll()
    void poll()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [])

  useEffect(() => {
    if (!state) return
    if (state.queueVersion !== queueVersion.current) {
      queueVersion.current = state.queueVersion
      api.queue().then((q) => setQueue(q.tracks)).catch(() => {})
    }
    // a scan finished: redraw the tree and reload the playlist; a playlist restored
    // after a restart cannot find its tracks until the scan completes. The count,
    // not the scanning flag: a scan from the meta cache can end between two polls.
    if (seenScans.current >= 0 && state.library.scans !== seenScans.current) {
      setTreeKey((k) => k + 1)
      api.queue().then((q) => setQueue(q.tracks)).catch(() => {})
    }
    seenScans.current = state.library.scans
  }, [state])

  useEffect(() => {
    const text = filter.trim()
    if (!text) {
      setSearchHits(null)
      return
    }
    const t = setTimeout(() => api.search(text).then(setSearchHits).catch(() => setSearchHits([])), 250)
    return () => clearTimeout(t)
  }, [filter])

  const run = useCallback(async (p: Promise<unknown>) => {
    try {
      await p
      setActionErr('')
    } catch (e) {
      setActionErr(e instanceof Error ? e.message : String(e))
    }
    refresh.current()
  }, [])
  const showErr = useCallback((m: string) => setActionErr(m), [])
  const [seekDrag, seekProps] = useHeld(state, stateAskedAt, (v) => run(api.seek(v)))
  const [volDrag, volProps] = useHeld(state, stateAskedAt, (v) => run(api.volume(v)))

  // spinners: these requests return only when the renderer has really switched,
  // which takes seconds. A later click owns the spinner; an earlier request ending
  // does not take it away.
  const [pendingPlay, setPendingPlay] = useState<number | null>(null)
  const [pendingRenderer, setPendingRenderer] = useState<string | null>(null)
  const playTrack = (i: number) => {
    setPendingPlay(i)
    void run(api.playIndex(i)).finally(() => setPendingPlay((p) => (p === i ? null : p)))
  }
  const useRenderer = (udn: string) => {
    setPendingRenderer(udn)
    void run(api.selectRenderer(udn)).finally(() => setPendingRenderer((p) => (p === udn ? null : p)))
  }

  const playingId = state?.track?.id ?? null
  // the playlist row clicked last; the properties panel shows it, or the playing
  // track while nothing in the current playlist is clicked
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const shownId = selectedId && queue.some((t) => t.id === selectedId) ? selectedId : playingId

  // "Select Opened File", as in JetBrains IDEs: show in the tree the node the playlist
  // was built from; a playlist built from search results has none, then the cue sheet
  // or the file of the playing track
  const [reveal, setReveal] = useState<RevealRequest | null>(null)
  const [collapseSeq, setCollapseSeq] = useState(0)
  const revealTarget = state?.source || state?.track?.leaf || ''
  // revealPath shows a node in the tree, leaving the search: the tree is hidden while
  // search results are shown
  const revealPath = useCallback((path: string) => {
    if (!path) return
    setFilter('')
    setReveal((r) => ({ path, seq: (r?.seq ?? 0) + 1 }))
  }, [])
  const revealPlaying = useCallback(() => revealPath(revealTarget), [revealPath, revealTarget])
  // once per page load: the tree opens at the playlist's source, as if Alt+F1 was pressed
  const revealedOnLoad = useRef(false)
  useEffect(() => {
    if (revealedOnLoad.current || !revealTarget) return
    revealedOnLoad.current = true
    revealPlaying()
  }, [revealTarget, revealPlaying])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.altKey && e.key === 'F1') {
        e.preventDefault()
        revealPlaying()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [revealPlaying])

  // arrow button in the tree: build the playlist from the node; playback is left alone
  const makePlaylist = (node: TreeNode) =>
    run(node.id ? api.setQueue([node.id], node.path) : api.tracks(node.path).then((tracks) => api.setQueue(tracks.map((t) => t.id), node.path)))

  const stalled = !!state?.stalled
  const playing = state?.transport === 'PLAYING' && !state?.paused && !stalled
  const duration = state?.duration || state?.track?.duration || 0
  const position = seekDrag ?? state?.position ?? 0
  const volume = volDrag ?? (state && state.volume >= 0 ? state.volume : 0)
  const track = state?.track
  const renderers = state?.renderers ?? []
  const current = renderers.find((r) => r.udn === state?.selected)
  const error = actionErr || state?.error || (stalled ? 'The renderer reports playing, but its position is not moving. It may be hung: restart it.' : '')
  const transport = connected ? (state?.waiting ? 'CONNECTING' : stalled ? 'STALLED' : state?.transport || 'IDLE') : 'BACKEND OFFLINE'

  return (
    <div className="app d-flex flex-column vh-100">
      {quitting && !connected && (
        <div className="alert alert-secondary m-2 mb-0 py-2">
          The player has stopped. Start it again from the desktop shortcut.
        </div>
      )}

      {/* columns with splitters; the library persists widths (localStorage, autoSaveId) */}
      <PanelGroup
        direction={narrow ? 'vertical' : 'horizontal'}
        autoSaveId={narrow ? 'go-upnp-player.layout.v' : 'go-upnp-player.layout.h'}
        className="flex-grow-1 min-h-0 p-2"
      >
        <Panel defaultSize={38} minSize={18} order={1}>
        {/* library */}
        <Card className="col-library h-100">
          {/* the search box is the card's title */}
          <Card.Header className="top-head d-flex align-items-center">
            <InputGroup size="sm" className="me-2 flex-grow-1 min-w-0">
              <Form.Control
                value={filter} placeholder="Search artist, album, title…"
                onChange={(e) => setFilter(e.target.value)}
              />
              {filter && <Button variant="outline-secondary" onClick={() => setFilter('')}>×</Button>}
            </InputGroup>
            {state?.library.scanning && <Spinner size="sm" className="me-2" />}
            {/* rescan the library folders for new or removed music; the tree keeps its
                expanded folders and the highlighted playlist source */}
            <Button
              size="sm" variant="outline-secondary" className="head-btn me-1"
              title="Refresh: rescan the library folders"
              disabled={state?.library.scanning} onClick={() => run(api.rescan())}
            >
              <RefreshIcon />
            </Button>
            {/* "Collapse All", as in JetBrains IDEs: two chevrons folding towards a line */}
            <Button
              size="sm" variant="outline-secondary" className="head-btn"
              title="Collapse all folders"
              onClick={() => {
                setFilter('') // the tree is hidden while search results are shown
                setCollapseSeq((s) => s + 1)
              }}
            >
              <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
                <path d="M4.5 2.5L8 5.5l3.5-3M4.5 13.5L8 10.5l3.5 3M2 8h12" />
              </svg>
            </Button>
          </Card.Header>
          <div className="flex-grow-1 overflow-auto min-h-0">
            {/* the tree is not unmounted during search, so expansion state is kept */}
            <div className={searchHits ? 'd-none' : ''}>
              <Tree selectedPath={state?.source ?? ''} onMakePlaylist={makePlaylist} reloadKey={treeKey} reveal={reveal} collapseSeq={collapseSeq} />
            </div>
            {searchHits && <SearchResults hits={searchHits} playingId={playingId} onMakePlaylist={(ids) => run(api.setQueue(ids))} onReveal={revealPath} />}
          </div>
        </Card>
        </Panel>
        <PanelResizeHandle className="split-handle" />
        <Panel defaultSize={34} minSize={18} order={2}>
        {/* tracks, and under them the properties of the clicked one */}
        <PanelGroup direction="vertical" autoSaveId="go-upnp-player.layout.playlist.v2">
        <Panel defaultSize={50} minSize={20} order={1}>
        <Card className="col-tracks h-100">
          {/* the playlist's own controls, no title: the jump to its source in the tree, the position
              of the playing track, then the transport on the right */}
          <Card.Header className="top-head d-flex align-items-center gap-2">
            <Button
              size="sm" variant="outline-secondary" className="head-btn flex-shrink-0"
              title="Show in the tree what the playlist was built from (Alt+F1)"
              disabled={!revealTarget} onClick={revealPlaying}
            >
              <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
                <circle cx="8" cy="8" r="5.2" />
                <circle cx="8" cy="8" r="1.4" fill="currentColor" />
                <path d="M8 0.8v2.4M8 12.8v2.4M0.8 8h2.4M12.8 8h2.4" />
              </svg>
            </Button>
            <small className="text-body-secondary font-monospace">{fmtTime(position)}</small>
            <Form.Range
              className={'flex-grow-1 seek' + (state?.off ? ' off' : '')} min={0} max={Math.max(1, duration)} step={1}
              value={Math.min(position, duration)} disabled={!track || !!state?.off}
              {...seekProps}
            />
            <small className="text-body-secondary font-monospace">{fmtTime(duration)}</small>
            <ButtonGroup className="flex-shrink-0">
              <Button size="sm" variant="outline-secondary" className="head-btn" title="Previous" onClick={() => run(api.prev())}>
                <BarIcon name="prev" />
              </Button>
              {playing ? (
                <Button size="sm" variant="outline-secondary" className="head-btn" title="Pause" onClick={() => run(api.pause())}>
                  <BarIcon name="pause" />
                </Button>
              ) : (
                <Button size="sm" variant="outline-secondary" className="head-btn" title="Play" onClick={() => run(api.resume())}>
                  <BarIcon name="play" />
                </Button>
              )}
              <Button size="sm" variant="outline-secondary" className="head-btn" title="Stop" onClick={() => run(api.stop())}>
                <BarIcon name="stop" />
              </Button>
              <Button size="sm" variant="outline-secondary" className="head-btn" title="Next" onClick={() => run(api.next())}>
                <BarIcon name="next" />
              </Button>
            </ButtonGroup>
          </Card.Header>
          <div className="flex-grow-1 overflow-auto min-h-0">
            <Playlist
              tracks={queue} playingId={playingId} pendingIndex={pendingPlay} onPlay={playTrack}
              selectedId={shownId} onSelect={setSelectedId}
            />
          </div>
        </Card>
        </Panel>
        <PanelResizeHandle className="split-handle" />
        <Panel defaultSize={50} minSize={10} order={2}>
        <Card className="col-tracks h-100">
          <Card.Header>Properties</Card.Header>
          <div className="flex-grow-1 overflow-auto min-h-0">
            <TrackProperties id={shownId} />
          </div>
        </Card>
        </Panel>
        </PanelGroup>
        </Panel>
        <PanelResizeHandle className="split-handle" />
        <Panel defaultSize={28} minSize={16} order={3}>
        {/* device, equalizer, folders */}
        <div className="col-side d-flex flex-column gap-2 overflow-auto h-100">
          {/* volume of the selected device, theme and Quit */}
          <Card>
            <Card.Header className="top-head d-flex align-items-center gap-2">
              <span>Vol</span>
              <Form.Range className="flex-grow-1" min={0} max={100} value={volume} title="Volume" {...volProps} />
              <small className="text-body-secondary vol-num">{state && state.volume >= 0 ? volume : '–'}</small>
              <Button
                size="sm" variant="outline-secondary" className="head-btn ms-2 flex-shrink-0"
                title="Check for updates" disabled={updateNote?.kind === 'checking' || updateNote?.kind === 'installing'}
                onClick={checkUpdate}
              >
                <UpdateIcon />
              </Button>
              <Button
                size="sm" variant="outline-secondary" className="head-btn flex-shrink-0"
                title={theme === 'dark' ? 'Switch to the light theme' : 'Switch to the dark theme'}
                onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
              >
                {theme === 'dark' ? (
                  // sun: what a click switches to
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
                    <circle cx="8" cy="8" r="3" />
                    <path d="M8 1v2M8 13v2M1 8h2M13 8h2M3 3l1.4 1.4M11.6 11.6L13 13M3 13l1.4-1.4M11.6 4.4L13 3" />
                  </svg>
                ) : (
                  // moon
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
                    <path d="M13.5 10.2A6 6 0 0 1 5.8 2.5a6 6 0 1 0 7.7 7.7z" />
                  </svg>
                )}
              </Button>
              {/* two-click quit, no modal dialogs: the first click arms (the button fills and the
                  status bar says to click again), the second shuts down; an icon only, square */}
              <Button
                size="sm" variant={quitArmed ? 'secondary' : 'outline-secondary'} className="head-btn flex-shrink-0"
                title={quitArmed ? 'Click again to quit' : 'Quit: stop the player program (the renderer is paused first)'}
                disabled={quitting}
                onClick={() => {
                  if (!quitArmed) {
                    setQuitArmed(true)
                    return
                  }
                  setQuitting(true)
                  run(api.shutdown())
                }}
              >
                {quitting ? <Spinner size="sm" className="head-spinner" /> : <BarIcon name="power" />}
              </Button>
            </Card.Header>
          </Card>
          <Card>
            <Card.Header className="d-flex align-items-center gap-2">
              <span>Output</span>
              {/* what goes out and from where: a property of the output as a whole */}
              {(current || track) && (
                <small className="flex-grow-1 min-w-0 text-body-secondary output-info">
                  {/* one line of parts with bold dots between them; a narrow column wraps
                      only at a dot, never inside a part */}
                  {[
                    ...(current ? [`from ${current.localIp}`] : []),
                    ...(track
                      ? [
                          `${track.sampleRate / 1000} kHz`,
                          `${track.bits} bit`,
                          track.channels === 2 ? 'stereo' : `${track.channels} ch`,
                          track.fromCue ? 'cue → WAV' : 'FLAC',
                        ]
                      : []),
                  ].map((part, i) => (
                    <span key={i} className="text-nowrap">
                      {i > 0 && <b className="output-dot">•</b>}
                      {part}
                    </span>
                  ))}
                </small>
              )}
              {/* the header speaks only for the player itself; the device's own state sits on its row */}
              {!connected && <Badge bg={transportVariant[transport]} className="me-2">{transport}</Badge>}
              <Button size="sm" variant="outline-secondary" className="head-btn ms-auto flex-shrink-0" title="Search for devices again" onClick={() => run(api.refreshRenderers())} disabled={state?.discovering}>
                {state?.discovering ? <Spinner size="sm" className="head-spinner" /> : <RefreshIcon />}
              </Button>
            </Card.Header>
            {/* devices as a list; select with a button that appears on hover after the name */}
            <ListGroup variant="flush">
              {renderers.length === 0 && (
                <ListGroup.Item className="text-body-secondary small">{state?.discovering ? 'Searching…' : 'No renderers found'}</ListGroup.Item>
              )}
              {renderers.map((r) => {
                const sel = r.udn === state?.selected
                return (
                  <ListGroup.Item key={r.udn} className={'renderer-row d-flex align-items-center gap-2' + (sel ? ' bg-body-secondary' : '')}>
                    <div className="min-w-0 flex-grow-1">
                      <div className="row-label">
                        <span className="renderer-ip font-monospace text-body-secondary">{r.ip}</span>
                        <span className={'text-truncate' + (sel ? ' fw-bold' : ' text-body-secondary')}>{r.name}</span>
                        {/* the selected device's transport state, right after its name: it is
                            this device that plays, stops or is being connected to */}
                        {sel && connected && (
                          <Badge
                            bg={transportVariant[transport] ?? 'idle'}
                            className={'ms-2 flex-shrink-0' + (transport === 'CONNECTING' ? ' badge-pulse' : '')}
                          >
                            {transportLabel[transport] ?? transport}
                          </Badge>
                        )}
                        {pendingRenderer === r.udn ? (
                          <Spinner size="sm" className="ms-2 flex-shrink-0 text-body-secondary" />
                        ) : !sel && (
                          <Button size="sm" variant="secondary" className="hover-btn" title="Play on this device" onClick={() => useRenderer(r.udn)}>
                            Use
                          </Button>
                        )}
                      </div>
                      <small className="text-body-secondary">{r.model || r.manufacturer}</small>
                    </div>
                  </ListGroup.Item>
                )
              })}
            </ListGroup>
            {error && (
              <Card.Body className="pt-2">
                <small className="text-danger d-block text-break">{error}</small>
              </Card.Body>
            )}
          </Card>
          <Equalizer onError={showErr} />
          <Settings scanning={!!state?.library.scanning} tracks={state?.library.tracks ?? 0} />
        </div>
        </Panel>
      </PanelGroup>
      {/* status bar: what the player is busy with on the left, its version on the right; no
          React or Vite versions, which only tell an outsider what the UI is built with */}
      <footer className="status-bar border-top bg-body-tertiary text-body-secondary d-flex align-items-center gap-3">
        <span className="status-msg text-truncate flex-grow-1" title={state?.waiting || ''}>
          {quitArmed && !quitting ? (
            <span className="status-blink">Click the power button again to quit the player.</span>
          ) : updateNote ? (
            <UpdateMessage note={updateNote} onClose={() => setUpdateNote(null)} onInstall={installUpdate} />
          ) : state?.waiting && (
            <>
              <Spinner size="sm" className="me-2" />
              {state.waiting}
            </>
          )}
        </span>
        <span className="flex-shrink-0">go-upnp-player {state?.version}</span>
      </footer>
    </div>
  )
}

// Search results: a compact list instead of the tree while the filter has text.
// The header buttons' icons: one size and one stroke, drawn like the theme button's sun and
// moon. Font symbols (⏮ ❚❚ ⏻) came out in different sizes and weights, some with
// colour fringes (2026-11-11).
function BarIcon({ name }: { name: 'prev' | 'play' | 'pause' | 'stop' | 'next' | 'power' }) {
  const shape = {
    prev: <><path d="M4 3v10" /><path d="M13 3.5v9L6.5 8z" fill="currentColor" /></>,
    play: <path d="M5 3v10l8-5z" fill="currentColor" />,
    pause: <><path d="M5.5 3.5v9M10.5 3.5v9" strokeWidth="2.2" /></>,
    stop: <rect x="3.5" y="3.5" width="9" height="9" rx="1" fill="currentColor" />,
    next: <><path d="M12 3v10" /><path d="M3 3.5v9L9.5 8z" fill="currentColor" /></>,
    power: <><path d="M8 1.8v5.4" /><path d="M4.6 4a5.2 5.2 0 1 0 6.8 0" /></>,
  }[name]
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" strokeLinecap="round" aria-hidden="true">
      {shape}
    </svg>
  )
}

// The answer to "Check for updates" in the status bar, with what to do about it and a ×.
// The answer blinks a few times so the eye finds it at the bottom of the window.
// A setup copy downloads the new setup, a go install copy rebuilds itself with go install,
// any other copy (the zip) gets the release page. Pages open in the system browser through
// the backend: a link inside the player's own window would open another such window.
function UpdateMessage({ note, onClose, onInstall }: {
  note: { kind: string; offer?: UpdateOffer; version?: string; message?: string }
  onClose: () => void
  onInstall: (version: string) => void
}) {
  const close = (
    <Button size="sm" variant="link" className="status-close text-body-secondary" title="Close" onClick={onClose}>×</Button>
  )
  const open = () => void api.openUpdate().catch(() => {})
  switch (note.kind) {
    case 'checking':
      return <><Spinner size="sm" className="me-2" />Checking for updates…</>
    case 'installing':
      return <><Spinner size="sm" className="me-2" />Updating to {note.version} with go install; the player restarts when it is built…</>
    case 'installed':
      return <span className="status-blink">go-upnp-player {note.version} is installed, but the player did not restart by itself: quit it and start it again.{close}</span>
    case 'latest':
      return <span className="status-blink" role="button" title="Click to close" onClick={onClose}>You have the latest version, {note.version}.{close}</span>
    case 'error':
      return <span className="text-danger">Could not check for updates: {note.message}{close}</span>
  }
  const o = note.offer!
  return (
    <span className="status-blink">
      go-upnp-player {o.latest} is available.
      {o.kind === 'go' ? (
        <Button size="sm" variant="link" className="status-link" title={o.command} onClick={() => onInstall(o.latest)}>Update with go install</Button>
      ) : (
        <Button size="sm" variant="link" className="status-link" onClick={open}>{o.kind === 'setup' ? 'Download the setup' : 'Open the release page'}</Button>
      )}
      {close}
    </span>
  )
}

// A click on a result shows it in the tree (its cue sheet or file), as the crosshair does
// for the playlist, and leaves the search; the → button makes a playlist of it.
function SearchResults({ hits, playingId, onMakePlaylist, onReveal }: {
  hits: Track[]; playingId: string | null; onMakePlaylist: (ids: string[]) => void; onReveal: (path: string) => void
}) {
  if (hits.length === 0) return <div className="p-3 text-body-secondary">Nothing found.</div>
  return (
    <div className="py-1">
      <div className="px-2 pb-1">
        <Button size="sm" variant="outline-secondary" onClick={() => onMakePlaylist(hits.map((t) => t.id))}>
          → Make a playlist of all {hits.length} results
        </Button>
      </div>
      {hits.map((t) => (
        <div
          key={t.id} className={'tree-row' + (t.id === playingId ? ' bg-body-secondary fw-bold' : ' text-body-secondary')}
          title={'Show in the tree: ' + t.leaf} role="button" onClick={() => onReveal(t.leaf)}
        >
          <span className="tree-num text-body-secondary">{t.number || ''}</span>
          <span className="tree-name text-truncate">
            {t.title} <small className="text-body-secondary">— {[t.artist, t.album].filter(Boolean).join(' · ')}</small>
          </span>
          <Button
            size="sm" variant="secondary" className="hover-btn icon-btn" title="Make a playlist of this track"
            onClick={(e) => {
              e.stopPropagation()
              onMakePlaylist([t.id])
            }}
          >
            <ToPlaylistIcon />
          </Button>
          <span className="ms-auto text-body-secondary tree-dur">{fmtTime(t.duration)}</span>
        </div>
      ))}
    </div>
  )
}

// Narrow screen (phone): columns stacked vertically, splitters horizontal.
function useNarrow(): boolean {
  const q = '(max-width: 900px)'
  const [narrow, setNarrow] = useState(() => window.matchMedia(q).matches)
  useEffect(() => {
    const m = window.matchMedia(q)
    const on = () => setNarrow(m.matches)
    m.addEventListener('change', on)
    return () => m.removeEventListener('change', on)
  }, [])
  return narrow
}

// A range input the poll must not overwrite while the user holds it: the held value
// shows instead of the polled one, and the release commits it. The release is caught
// on the window, not on the input: a button let go outside the slider, a cancelled
// pointer (alt-tab, touch turned into a scroll) or the arrow keys never sent the
// input its pointerup, and the old onPointerUp-only code then kept the held value
// forever, a slider frozen until the page was reloaded (2026-11-10).
//
// After the release the slider shows the chosen value until the first state asked for
// after the command answered: from then on the backend's value, whatever it is. The
// page does not judge it: the backend reports the seek target until the renderer has
// caught up with it (services.SeekLanded); guessing that here, by how near the polled
// value came to the chosen one, made the knob jump on its own (2026-11-11).
function useHeld(sample: unknown, sampleAskedAt: { current: number }, commit: (v: number) => Promise<unknown>) {
  const [held, setHeld] = useState<number | null>(null)
  const down = useRef(false)
  const last = useRef<number | null>(null)
  const pending = useRef<{ answered: number } | null>(null)
  const commitRef = useRef(commit)
  commitRef.current = commit
  const release = useCallback(() => {
    down.current = false
    const v = last.current
    last.current = null
    if (v === null) return // a release elsewhere on the page, or nothing was moved
    const p = { answered: 0 }
    pending.current = p
    setHeld(v)
    void commitRef.current(v).finally(() => {
      p.answered = Date.now()
    })
  }, [])
  // a state asked for after the answer: the poll owns the slider again
  useEffect(() => {
    const p = pending.current
    if (!p || !p.answered || down.current || sampleAskedAt.current < p.answered) return
    pending.current = null
    setHeld(null)
  }, [sample]) // once per poll
  useEffect(() => {
    window.addEventListener('pointerup', release)
    window.addEventListener('pointercancel', release)
    window.addEventListener('blur', release)
    return () => {
      window.removeEventListener('pointerup', release)
      window.removeEventListener('pointercancel', release)
      window.removeEventListener('blur', release)
    }
  }, [release])
  const props = {
    onPointerDown: () => {
      down.current = true
      pending.current = null // a new grab: an earlier command no longer owns the slider
    },
    onChange: (e: ChangeEvent<HTMLInputElement>) => {
      last.current = Number(e.target.value)
      if (down.current) setHeld(last.current)
      else release() // keyboard: every step is a release
    },
  }
  return [held, props] as const
}
