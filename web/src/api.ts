export interface Track {
  id: string
  leaf: string
  path: string
  number: number
  title: string
  artist: string
  album: string
  date: string
  // the album's year, from its folders; the date tag of a reissue is the reissue's
  year?: string
  // what an album header shows: the album's year, then the release's after a slash
  years?: string
  genre: string
  sampleRate: number
  channels: number
  bits: number
  duration: number
  fromCue: boolean
  dsdRate?: number // a DSD file: its own rate; sampleRate and bits are the PCM it plays as
}

export interface TreeNode {
  path: string
  name: string
  kind: 'dir' | 'cue' | 'file' | 'track'
  count: number
  id?: string
  number: number
  duration?: number
  starred?: boolean // folders only: marked with a star
}

export interface Renderer {
  udn: string
  name: string
  manufacturer: string
  model: string
  localIp: string
  ip: string
}

export interface PlayerState {
  renderers: Renderer[] | null
  selected: string
  transport: string
  stalled: boolean
  wantPlaying: boolean
  // the playlist played out and the renderer was stopped: no stream, no position
  off?: boolean
  paused: boolean
  position: number
  duration: number
  volume: number
  index: number
  queueVersion: number
  queueLen: number
  source: string // tree node the playlist was built from, '' if none
  track: Track | null
  error?: string
  waiting?: string // the renderer refused to play; the start is being retried
  discovering: boolean
  autoScan: boolean // the Output card's Scan switch: a device search every second
  dsdApplying?: boolean // a DSD card change is being applied (rescan, restart of the track)
  library: { scanning: boolean; tracks: number; scans: number }
  build?: string
  version?: string
  dev?: string // the -web development run: the address it listens on
}

// the answer to "check for updates"; kind says how this copy is updated
export interface UpdateOffer {
  current: string
  latest: string
  newer: boolean
  kind: 'setup' | 'go' | 'page'
  url?: string
  command?: string
}

async function req<T>(method: string, url: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const r = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!r.ok) {
    throw new Error((await r.text()) || `${r.status} ${r.statusText}`)
  }
  if (r.status === 204 || r.status === 202) return undefined as T
  return r.json() as Promise<T>
}

const q = (path: string) => encodeURIComponent(path)

export const api = {
  // the page polls state one request at a time: a reply that never comes would stop
  // the poll for good (frozen slider and badges), so it is given up after 5 s
  state: () => req<PlayerState>('GET', '/api/state', undefined, AbortSignal.timeout(5000)),
  tree: (path = '') => req<TreeNode[]>('GET', `/api/tree?path=${q(path)}`),
  setStarred: (path: string, on: boolean) => req<void>('PUT', '/api/starred', { path, on }),
  tracks: (path: string) => req<Track[]>('GET', `/api/tracks?path=${q(path)}`),
  search: (text: string) => req<Track[]>('GET', `/api/search?q=${q(text)}`),
  queue: () => req<{ version: number; tracks: Track[] }>('GET', '/api/queue'),
  // source: the tree node the playlist is built from ('' for search results)
  setQueue: (ids: string[], source = '') => req<void>('PUT', '/api/queue', { ids, source }),
  play: (ids: string[], index: number) => req<void>('POST', '/api/player/play', { ids, index }),
  playIndex: (index: number) => req<void>('POST', '/api/player/play', { index }),
  pause: () => req<void>('POST', '/api/player/pause'),
  resume: () => req<void>('POST', '/api/player/resume'),
  stop: () => req<void>('POST', '/api/player/stop'),
  next: () => req<void>('POST', '/api/player/next'),
  prev: () => req<void>('POST', '/api/player/prev'),
  seek: (value: number) => req<void>('POST', '/api/player/seek', { value }),
  volume: (value: number) => req<void>('POST', '/api/player/volume', { value }),
  refreshRenderers: () => req<void>('POST', '/api/renderers/refresh'),
  autoScanRenderers: (on: boolean) => req<void>('PUT', '/api/renderers/auto', { on }),
  selectRenderer: (udn: string) => req<void>('POST', '/api/renderers/select', { udn }),
  settings: () => req<{ roots: string[] }>('GET', '/api/settings'),
  saveSettings: (roots: string[]) => req<void>('PUT', '/api/settings', { roots }),
  rescan: () => req<void>('POST', '/api/library/rescan'),
  shutdown: () => req<void>('POST', '/api/shutdown'),
  properties: (id: string) => req<TrackProps>('GET', `/api/track/${q(id)}/properties`),
  eq: () => req<EQ>('GET', '/api/eq'),
  checkUpdate: () => req<UpdateOffer>('GET', '/api/update'),
  openUpdate: () => req<void>('POST', '/api/update/open'),
  installUpdate: () => req<void>('POST', '/api/update/install'),
  saveEq: (enabled: boolean, gains: number[]) => req<void>('PUT', '/api/eq', { enabled, gains }),
  dsd: () => req<DSDSettings>('GET', '/api/dsd'),
  saveDsd: (rate: number, gainDb: number) => req<void>('PUT', '/api/dsd', { rate, gainDb }),
}

// how DSD files are converted to PCM, and the choices
export interface DSDSettings {
  rate: number
  gainDb: number
  rates: number[]
  gains: number[]
}

// what the properties panel under the playlist shows
export interface TrackProps {
  number: number
  title: string
  artist: string
  album: string
  date: string
  genre: string
  comment: string
  duration: number
  samples: number
  sampleRate: number
  channels: number
  bits: number
  bitrateKbps: number
  codec: string
  encoding: string
  tool: string
  embeddedCue: boolean
  md5: string
}

export interface EQ {
  enabled: boolean
  gains: number[]
  bands: number[]
  maxGain: number
}

export function fmtTime(sec: number): string {
  if (!isFinite(sec) || sec < 0) sec = 0
  const s = Math.floor(sec)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = String(s % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

export const artUrl = (id: string) => `/media/art/${id}`
