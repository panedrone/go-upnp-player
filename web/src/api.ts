export interface Track {
  id: string
  leaf: string
  path: string
  number: number
  title: string
  artist: string
  album: string
  date: string
  genre: string
  sampleRate: number
  channels: number
  bits: number
  duration: number
  fromCue: boolean
}

export interface TreeNode {
  path: string
  name: string
  kind: 'dir' | 'cue' | 'file' | 'track'
  count: number
  id?: string
  number: number
  duration?: number
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
  wantPlaying: boolean
  paused: boolean
  position: number
  duration: number
  volume: number
  index: number
  queueVersion: number
  queueLen: number
  track: Track | null
  error?: string
  discovering: boolean
  library: { scanning: boolean; tracks: number }
}

async function req<T>(method: string, url: string, body?: unknown): Promise<T> {
  const r = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!r.ok) {
    throw new Error((await r.text()) || `${r.status} ${r.statusText}`)
  }
  if (r.status === 204 || r.status === 202) return undefined as T
  return r.json() as Promise<T>
}

const q = (path: string) => encodeURIComponent(path)

export const api = {
  tree: (path = '') => req<TreeNode[]>('GET', `/api/tree?path=${q(path)}`),
  tracks: (path: string) => req<Track[]>('GET', `/api/tracks?path=${q(path)}`),
  search: (text: string) => req<Track[]>('GET', `/api/search?q=${q(text)}`),
  queue: () => req<{ version: number; tracks: Track[] }>('GET', '/api/queue'),
  setQueue: (ids: string[]) => req<void>('PUT', '/api/queue', { ids }),
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
  selectRenderer: (udn: string) => req<void>('POST', '/api/renderers/select', { udn }),
  settings: () => req<{ roots: string[] }>('GET', '/api/settings'),
  saveSettings: (roots: string[]) => req<void>('PUT', '/api/settings', { roots }),
  rescan: () => req<void>('POST', '/api/library/rescan'),
  shutdown: () => req<void>('POST', '/api/shutdown'),
  eq: () => req<EQ>('GET', '/api/eq'),
  saveEq: (enabled: boolean, gains: number[]) => req<void>('PUT', '/api/eq', { enabled, gains }),
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
