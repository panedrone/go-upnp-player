import { useCallback, useEffect, useRef, useState } from 'react'
import { Badge, Button, ButtonGroup, Card, Form, Image, InputGroup, ListGroup, Navbar, Spinner } from 'react-bootstrap'
import { api, artUrl, fmtTime, PlayerState, Track, TreeNode } from './api'
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels'
import Tree from './Tree'
import Playlist from './Playlist'
import Equalizer from './Equalizer'
import Settings from './Settings'

const transportVariant: Record<string, string> = {
  PLAYING: 'success',
  PAUSED_PLAYBACK: 'warning',
  TRANSITIONING: 'info',
  OFFLINE: 'danger',
  'BACKEND OFFLINE': 'danger',
}

export default function App() {
  const [state, setState] = useState<PlayerState | null>(null)
  const [connected, setConnected] = useState(false)
  const [queue, setQueue] = useState<Track[]>([])
  const [filter, setFilter] = useState('')
  const [searchHits, setSearchHits] = useState<Track[] | null>(null)
  const [seekDrag, setSeekDrag] = useState<number | null>(null)
  const [volDrag, setVolDrag] = useState<number | null>(null)
  const [actionErr, setActionErr] = useState('')
  const [treeKey, setTreeKey] = useState(0)
  const [quitArmed, setQuitArmed] = useState(false)
  const [quitting, setQuitting] = useState(false)

  // an "armed" Quit button disarms itself after 3 s
  useEffect(() => {
    if (!quitArmed) return
    const t = setTimeout(() => setQuitArmed(false), 3000)
    return () => clearTimeout(t)
  }, [quitArmed])
  const narrow = useNarrow()
  const queueVersion = useRef(-1)
  const wasScanning = useRef(false)

  // player state via SSE; EventSource reconnects on its own
  useEffect(() => {
    const es = new EventSource('/api/events')
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = (e) => setState(JSON.parse(e.data))
    return () => es.close()
  }, [])

  useEffect(() => {
    if (!state) return
    if (state.queueVersion !== queueVersion.current) {
      queueVersion.current = state.queueVersion
      api.queue().then((q) => setQueue(q.tracks)).catch(() => {})
    }
    // scan finished: redraw the tree and reload the playlist; a playlist restored
    // after a restart cannot find its tracks until the scan completes
    if (wasScanning.current && !state.library.scanning) {
      setTreeKey((k) => k + 1)
      api.queue().then((q) => setQueue(q.tracks)).catch(() => {})
    }
    wasScanning.current = state.library.scanning
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
  }, [])
  const showErr = useCallback((m: string) => setActionErr(m), [])

  const playingId = state?.track?.id ?? null

  // arrow button in the tree: build the playlist from the node; playback is left alone
  const makePlaylist = (node: TreeNode) =>
    run(node.id ? api.setQueue([node.id]) : api.tracks(node.path).then((tracks) => api.setQueue(tracks.map((t) => t.id))))

  const playing = state?.transport === 'PLAYING' && !state?.paused
  const duration = state?.duration || state?.track?.duration || 0
  const position = seekDrag ?? state?.position ?? 0
  const volume = volDrag ?? (state && state.volume >= 0 ? state.volume : 0)
  const track = state?.track
  const renderers = state?.renderers ?? []
  const current = renderers.find((r) => r.udn === state?.selected)
  const error = actionErr || state?.error
  const transport = connected ? state?.transport || 'IDLE' : 'BACKEND OFFLINE'

  return (
    <div className="app d-flex flex-column vh-100">
      {/* player: now playing, transport, position, volume */}
      <Navbar bg="body-tertiary" className="border-bottom px-3 py-2 gap-3 flex-nowrap player-bar">
        <div className="d-flex align-items-center gap-2 now-playing">
          {track ? (
            <>
              <Image
                src={artUrl(track.id)} rounded className="now-thumb" alt=""
                onError={(e) => ((e.target as HTMLImageElement).style.visibility = 'hidden')}
                onLoad={(e) => ((e.target as HTMLImageElement).style.visibility = 'visible')}
              />
              <div className="min-w-0">
                <div className="fw-semibold text-truncate" title={track.title}>{track.title}</div>
                <small className="text-body-secondary text-truncate d-block" title={track.path}>
                  {[track.artist, track.album].filter(Boolean).join(' — ')}
                </small>
              </div>
            </>
          ) : (
            <span className="text-body-secondary">Nothing is playing</span>
          )}
        </div>

        <ButtonGroup>
          <Button variant="outline-secondary" title="Previous" onClick={() => run(api.prev())}>⏮</Button>
          {playing ? (
            <Button variant="primary" title="Pause" onClick={() => run(api.pause())}>❚❚</Button>
          ) : (
            <Button variant="primary" title="Play" onClick={() => run(api.resume())}>▶</Button>
          )}
          <Button variant="outline-secondary" title="Stop" onClick={() => run(api.stop())}>■</Button>
          <Button variant="outline-secondary" title="Next" onClick={() => run(api.next())}>⏭</Button>
        </ButtonGroup>

        <div className="d-flex align-items-center gap-2 flex-grow-1 seek-wrap">
          <small className="text-body-secondary font-monospace">{fmtTime(position)}</small>
          <Form.Range
            className="flex-grow-1" min={0} max={Math.max(1, duration)} step={1}
            value={Math.min(position, duration)} disabled={!track}
            onChange={(e) => setSeekDrag(Number(e.target.value))}
            onPointerUp={() => {
              if (seekDrag !== null) run(api.seek(seekDrag))
              setSeekDrag(null)
            }}
          />
          <small className="text-body-secondary font-monospace">{fmtTime(duration)}</small>
        </div>

        <div className="d-flex align-items-center gap-2 vol" title="Volume">
          <small className="text-body-secondary">Vol</small>
          <Form.Range
            min={0} max={100} value={volume}
            onChange={(e) => setVolDrag(Number(e.target.value))}
            onPointerUp={() => {
              if (volDrag !== null) run(api.volume(volDrag))
              setVolDrag(null)
            }}
          />
          <small className="text-body-secondary vol-num">{state && state.volume >= 0 ? volume : '–'}</small>
        </div>

        {/* two-click quit, no modal dialogs: the first click arms, the second shuts down */}
        <Button
          size="sm" variant={quitArmed ? 'danger' : 'outline-secondary'} className="text-nowrap"
          title="Stop the player program (the renderer is paused first)"
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
          {quitting ? 'Stopping…' : quitArmed ? 'Click again to quit' : '⏻ Quit'}
        </Button>
      </Navbar>

      {quitting && !connected && (
        <div className="alert alert-secondary m-2 mb-0 py-2">
          The player has stopped. Start it again from the desktop shortcut.
        </div>
      )}

      {/* columns with splitters; the library persists widths (localStorage, autoSaveId) */}
      <PanelGroup
        direction={narrow ? 'vertical' : 'horizontal'}
        autoSaveId={narrow ? 'upnp-player.layout.v' : 'upnp-player.layout.h'}
        className="flex-grow-1 min-h-0 p-2"
      >
        <Panel defaultSize={38} minSize={18} order={1}>
        {/* library */}
        <Card className="col-library h-100">
          <Card.Header className="d-flex align-items-center">
            <span className="me-auto">Library</span>
            {state?.library.scanning && <Spinner size="sm" />}
          </Card.Header>
          <div className="p-2 border-bottom">
            <InputGroup size="sm">
              <Form.Control
                value={filter} placeholder="Search artist, album, title…"
                onChange={(e) => setFilter(e.target.value)}
              />
              {filter && <Button variant="outline-secondary" onClick={() => setFilter('')}>×</Button>}
            </InputGroup>
          </div>
          <div className="flex-grow-1 overflow-auto min-h-0">
            {/* the tree is not unmounted during search, so expansion state is kept */}
            <div className={searchHits ? 'd-none' : ''}>
              <Tree playingId={playingId} onMakePlaylist={makePlaylist} reloadKey={treeKey} />
            </div>
            {searchHits && <SearchResults hits={searchHits} playingId={playingId} onMakePlaylist={(ids) => run(api.setQueue(ids))} />}
          </div>
        </Card>
        </Panel>
        <PanelResizeHandle className="split-handle" />
        <Panel defaultSize={34} minSize={18} order={2}>
        {/* tracks */}
        <Card className="col-tracks h-100">
          <Card.Header className="d-flex align-items-center">
            <span className="me-auto">Playlist</span>
            <Badge bg="secondary" pill>{queue.length}</Badge>
          </Card.Header>
          <div className="flex-grow-1 overflow-auto min-h-0">
            <Playlist tracks={queue} playingId={playingId} onPlay={(i) => run(api.playIndex(i))} />
          </div>
        </Card>
        </Panel>
        <PanelResizeHandle className="split-handle" />
        <Panel defaultSize={28} minSize={16} order={3}>
        {/* device, equalizer, folders */}
        <div className="col-side d-flex flex-column gap-2 overflow-auto h-100">
          <Card>
            <Card.Header className="d-flex align-items-center">
              <span className="me-auto">Output</span>
              <Badge bg={transportVariant[transport] ?? 'secondary'} className="me-2">{transport}</Badge>
              <Button size="sm" variant="outline-secondary" className="py-0" title="Search for devices again" onClick={() => run(api.refreshRenderers())} disabled={state?.discovering}>
                {state?.discovering ? <Spinner size="sm" /> : '⟳'}
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
                  <ListGroup.Item key={r.udn} className={'renderer-row d-flex align-items-center gap-2' + (sel ? ' selected' : '')}>
                    <span className={sel ? 'text-primary' : 'text-body-secondary'}>{sel ? '●' : '○'}</span>
                    <div className="min-w-0">
                      <div className="row-label">
                        <span className="renderer-ip font-monospace text-body-secondary">{r.ip}</span>
                        <span className={'text-truncate' + (sel ? ' fw-semibold' : '')}>{r.name}</span>
                        {!sel && (
                          <Button size="sm" variant="primary" className="hover-btn" title="Play on this device" onClick={() => run(api.selectRenderer(r.udn))}>
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
            <Card.Body className="pt-2">
              {current && <small className="text-body-secondary d-block">streams from {current.localIp}</small>}
              {track && (
                <small className="text-body-secondary d-block">
                  {track.sampleRate / 1000} kHz · {track.bits} bit · {track.channels === 2 ? 'stereo' : `${track.channels} ch`} · {track.fromCue ? 'cue → WAV' : 'FLAC'}
                </small>
              )}
              {error && <small className="text-danger d-block mt-2 text-break">{error}</small>}
            </Card.Body>
          </Card>
          <Equalizer onError={showErr} />
          <Settings scanning={!!state?.library.scanning} tracks={state?.library.tracks ?? 0} />
        </div>
        </Panel>
      </PanelGroup>
    </div>
  )
}

// Search results: a compact list instead of the tree while the filter has text.
function SearchResults({ hits, playingId, onMakePlaylist }: { hits: Track[]; playingId: string | null; onMakePlaylist: (ids: string[]) => void }) {
  if (hits.length === 0) return <div className="p-3 text-body-secondary">Nothing found.</div>
  return (
    <div className="py-1">
      <div className="px-2 pb-1">
        <Button size="sm" variant="outline-primary" onClick={() => onMakePlaylist(hits.map((t) => t.id))}>
          → Make a playlist of all {hits.length} results
        </Button>
      </div>
      {hits.map((t) => (
        <div key={t.id} className={'tree-row' + (t.id === playingId ? ' playing' : '')} title={t.leaf}>
          <span className="tree-num text-body-secondary">{t.id === playingId ? '▶' : t.number || ''}</span>
          <span className="tree-name text-truncate">
            {t.title} <small className="text-body-secondary">— {[t.artist, t.album].filter(Boolean).join(' · ')}</small>
          </span>
          <Button size="sm" variant="primary" className="hover-btn" title="Make a playlist of this track" onClick={() => onMakePlaylist([t.id])}>
            →
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
