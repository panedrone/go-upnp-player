import { useEffect, useState } from 'react'
import { Button, Spinner } from 'react-bootstrap'
import { api, fmtTime, TreeNode } from './api'

// Expanded tree nodes live in the browser localStorage, so they survive a page
// reload and an app restart. Storage may be unavailable (private window,
// site data blocked); then we just work without persistence.
const OPEN_KEY = 'go-upnp-player.tree.open'
// key used before the project rename (2026-11-09); read once as a fallback
const LEGACY_OPEN_KEY = 'upnp-player.tree.open'

// hadSavedState: the key already exists (even if empty: the user collapsed everything)
const [openPaths, hadSavedState] = ((): [Set<string>, boolean] => {
  try {
    const raw = localStorage.getItem(OPEN_KEY) ?? localStorage.getItem(LEGACY_OPEN_KEY)
    return [new Set<string>(raw ? JSON.parse(raw) : []), raw !== null]
  } catch {
    return [new Set<string>(), false]
  }
})()

function rememberOpen(path: string, open: boolean) {
  if (open) openPaths.add(path)
  else openPaths.delete(path)
  try {
    localStorage.setItem(OPEN_KEY, JSON.stringify([...openPaths]))
  } catch {
    // no storage: not a problem
  }
}

// RevealRequest asks the tree to show a track ("Select Opened File" in JetBrains
// IDEs): expand the folders down to it, scroll to it and flash the row. seq makes
// a repeated request for the same track fire again.
export interface RevealRequest {
  id: string // track id, matches data-id on the row
  leaf: string // the cue sheet or the file that holds the track
  fromCue: boolean // the track sits inside leaf, which must be expanded too
  seq: number
}

interface Props {
  playingId: string | null
  // arrow button: build a playlist from the node (folder, cue, file, track)
  onMakePlaylist: (node: TreeNode) => void
  reloadKey: number
  reveal?: RevealRequest | null
  revealTick?: number // internal: bumped after paths were added to openPaths
}

// revealChain lists the tree paths to expand to show a track: every folder from
// the library root down to the leaf's folder, plus the leaf itself for cue tracks.
function revealChain(roots: TreeNode[], leaf: string, fromCue: boolean): string[] {
  const sep = '\\'
  const low = leaf.toLowerCase()
  const root = roots.find((r) => low.startsWith(r.path.toLowerCase() + sep))
  if (!root) return []
  const parts = leaf.slice(root.path.length + 1).split(sep)
  const chain = [root.path]
  let cur = root.path
  for (const p of parts.slice(0, -1)) {
    cur = cur + sep + p
    chain.push(cur)
  }
  if (fromCue) chain.push(leaf)
  return chain
}

// Tree following the folder structure: only what has something playable. A cue expands
// into its tracks, track files are leaves. Nothing plays from the tree: the arrow button
// on a row builds a playlist from the node, and the playlist is what plays.
export default function Tree({ playingId, onMakePlaylist, reloadKey, reveal }: Props) {
  const [roots, setRoots] = useState<TreeNode[] | null>(null)
  const [revealTick, setRevealTick] = useState(0)

  useEffect(() => {
    if (!reveal || !roots) return
    for (const p of revealChain(roots, reveal.leaf, reveal.fromCue)) rememberOpen(p, true)
    setRevealTick((t) => t + 1)
    // folders load lazily, one level per request: wait for the row to appear
    let tries = 0
    const timer = setInterval(() => {
      const row = document.querySelector<HTMLElement>(`.tree-row[data-id="${reveal.id}"]`)
      // wait until it is laid out too: the tree may still be hidden behind search results
      const ready = row !== null && row.offsetParent !== null
      if (ready || ++tries > 40) {
        clearInterval(timer)
        if (row && ready) {
          row.scrollIntoView({ block: 'center' })
          row.classList.remove('revealed')
          void row.offsetWidth // restart the flash animation
          row.classList.add('revealed')
        }
      }
    }, 100)
    return () => clearInterval(timer)
  }, [reveal, roots])

  useEffect(() => {
    api
      .tree()
      .then((r) => {
        // first run: roots are expanded; persist that, otherwise after a reload
        // saved subfolders would end up under a collapsed root
        if (!hadSavedState) r.forEach((n) => rememberOpen(n.path, true))
        setRoots(r)
      })
      .catch(() => setRoots([]))
  }, [reloadKey])

  if (roots === null) {
    return <div className="p-3"><Spinner size="sm" /></div>
  }
  if (roots.length === 0) {
    return <div className="p-3 text-body-secondary">Library is empty. Add music folders on the right.</div>
  }
  return (
    <div className="tree py-1">
      <Level nodes={roots} depth={0} playingId={playingId} onMakePlaylist={onMakePlaylist} reloadKey={reloadKey} revealTick={revealTick} rootLevel />
    </div>
  )
}

interface LevelProps extends Props {
  nodes: TreeNode[]
  depth: number
  rootLevel?: boolean
}

function Level({ nodes, rootLevel, ...rest }: LevelProps) {
  return (
    <>
      {nodes.map((n) => (
        <TreeItem key={n.path} node={n} initiallyOpen={rootLevel} {...rest} />
      ))}
    </>
  )
}

interface ItemProps extends Props {
  node: TreeNode
  depth: number
  initiallyOpen?: boolean
}

// Monochrome outline icons drawn in the muted text colour (currentColor): colour
// emoji (yellow folders, a rainbow disc) were too loud for the tree.
function NodeIcon({ kind }: { kind: TreeNode['kind'] }) {
  const common = { width: 14, height: 14, viewBox: '0 0 16 16', fill: 'none', stroke: 'currentColor', strokeWidth: 1.3 }
  switch (kind) {
    case 'dir':
      return (
        <svg {...common} aria-hidden="true">
          <path d="M1.5 4.5v8a1 1 0 0 0 1 1h11a1 1 0 0 0 1-1V6a1 1 0 0 0-1-1H8L6.5 3h-4a1 1 0 0 0-1 1.5z" />
        </svg>
      )
    case 'cue':
      return (
        <svg {...common} aria-hidden="true">
          <circle cx="8" cy="8" r="6.3" />
          <circle cx="8" cy="8" r="1.6" />
        </svg>
      )
    case 'file':
      return (
        <svg {...common} aria-hidden="true">
          <path d="M6 12.5V3.5l7-1.5v9" />
          <circle cx="4.3" cy="12.5" r="1.7" />
          <circle cx="11.3" cy="11" r="1.7" />
        </svg>
      )
    default:
      return null
  }
}

function TreeItem({ node, depth, playingId, onMakePlaylist, initiallyOpen, reloadKey, revealTick }: ItemProps) {
  const expandable = node.kind === 'dir' || node.kind === 'cue'
  // roots are expanded by default until the user has saved anything
  const [open, setOpenState] = useState(openPaths.has(node.path) || (!!initiallyOpen && !hadSavedState))
  const setOpen = (v: boolean) => {
    setOpenState(v)
    rememberOpen(node.path, v)
  }
  const [children, setChildren] = useState<TreeNode[] | null>(null)

  useEffect(() => {
    setChildren(null)
  }, [reloadKey])

  // a reveal request added this node to openPaths: expand it
  useEffect(() => {
    if (expandable && openPaths.has(node.path)) setOpenState(true)
  }, [revealTick, expandable, node.path])

  useEffect(() => {
    if (open && expandable && children === null) {
      api.tree(node.path).then(setChildren).catch(() => setChildren([]))
    }
  }, [open, expandable, children, node.path])

  const playing = !!node.id && node.id === playingId

  return (
    <>
      <div
        className={'tree-row' + (playing ? ' playing' : '')}
        data-id={node.id}
        style={{ paddingLeft: 6 + depth * 16 }}
        onDoubleClick={() => expandable && setOpen(!open)}
        title={node.kind === 'track' ? node.name : node.path}
      >
        <span
          className="tree-toggle text-body-secondary"
          onClick={(e) => {
            e.stopPropagation()
            if (expandable) setOpen(!open)
          }}
        >
          {expandable ? (open ? '▾' : '▸') : ''}
        </span>
        {/* the playing node is marked by its row highlight only; the number stays */}
        {node.kind === 'track' ? (
          <span className="tree-num text-body-secondary">{node.number > 0 ? node.number : ''}</span>
        ) : (
          <span className="tree-icon text-body-secondary"><NodeIcon kind={node.kind} /></span>
        )}
        <span className="tree-name text-truncate">{node.name}</span>
        {/* the button appears on hover, right after the label */}
        <Button
          size="sm" variant="primary" className="hover-btn"
          title={expandable ? 'Make a playlist of everything inside' : 'Make a playlist of this track'}
          onClick={(e) => {
            e.stopPropagation()
            onMakePlaylist(node)
          }}
        >
          →
        </Button>
        <span className="ms-auto text-body-secondary tree-dur">{node.duration ? fmtTime(node.duration) : ''}</span>
      </div>
      {open && children && (
        <Level nodes={children} depth={depth + 1} playingId={playingId} onMakePlaylist={onMakePlaylist} reloadKey={reloadKey} revealTick={revealTick} />
      )}
    </>
  )
}
