import { useEffect, useRef, useState } from 'react'
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

// RevealRequest asks the tree to show a node ("Select Opened File" in JetBrains
// IDEs): expand the folders down to it, scroll to it and flash the row. seq makes
// a repeated request for the same node fire again.
export interface RevealRequest {
  path: string // a folder, a cue sheet or a file; matches data-path on the row
  seq: number
}

interface Props {
  // the node the playlist was built from: its row stays highlighted, as the
  // selection in foobar2000's album list shows what the playlist came from
  selectedPath: string
  // arrow button: build a playlist from the node (folder, cue, file, track)
  onMakePlaylist: (node: TreeNode) => void
  reloadKey: number
  reveal?: RevealRequest | null
  // "Collapse All" as in JetBrains IDEs: every change collapses everything below the
  // library roots (the roots stay open, otherwise the tree shrinks to a single row)
  collapseSeq?: number
  syncTick?: number // internal: bumped after openPaths was changed from outside the items
}

// revealChain lists the tree paths to expand to show a node: every folder from the
// library root down to the node's parent. The node itself stays as it is.
function revealChain(roots: TreeNode[], path: string): string[] {
  // paths come from the server's OS: backslashes on Windows, slashes elsewhere
  const sep = path.includes('\\') ? '\\' : '/'
  const low = path.toLowerCase()
  const root = roots.find((r) => low.startsWith(r.path.toLowerCase() + sep))
  if (!root) return []
  const parts = path.slice(root.path.length + 1).split(sep)
  const chain = [root.path]
  let cur = root.path
  for (const p of parts.slice(0, -1)) {
    cur = cur + sep + p
    chain.push(cur)
  }
  return chain
}

// Tree following the folder structure: only what has something playable. Cue sheets and
// track files are leaves: their tracks are what the playlist shows, listing them here too
// only repeated it. Nothing plays from the tree: the arrow button on a row builds a
// playlist from the node, and the playlist is what plays.
export default function Tree({ selectedPath, onMakePlaylist, reloadKey, reveal, collapseSeq }: Props) {
  const [roots, setRoots] = useState<TreeNode[] | null>(null)
  const [syncTick, setSyncTick] = useState(0)
  const top = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!collapseSeq || !roots) return
    for (const p of [...openPaths]) rememberOpen(p, false)
    roots.forEach((r) => rememberOpen(r.path, true))
    setSyncTick((t) => t + 1)
    top.current?.scrollIntoView({ block: 'start' })
  }, [collapseSeq]) // only on a new request, not when roots reload

  useEffect(() => {
    if (!reveal || !roots) return
    for (const p of revealChain(roots, reveal.path)) rememberOpen(p, true)
    setSyncTick((t) => t + 1)
    // folders load lazily, one level per request: wait for the row to appear
    let tries = 0
    const timer = setInterval(() => {
      const row = [...document.querySelectorAll<HTMLElement>('.tree-row')].find((r) => r.dataset.path === reveal.path) ?? null
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
    <div className="tree py-1" ref={top}>
      <Level nodes={roots} depth={0} selectedPath={selectedPath} onMakePlaylist={onMakePlaylist} reloadKey={reloadKey} syncTick={syncTick} rootLevel />
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

function TreeItem({ node, depth, selectedPath, onMakePlaylist, initiallyOpen, reloadKey, syncTick }: ItemProps) {
  const expandable = node.kind === 'dir'
  // roots are expanded by default until the user has saved anything
  const [open, setOpenState] = useState(openPaths.has(node.path) || (!!initiallyOpen && !hadSavedState))
  const setOpen = (v: boolean) => {
    setOpenState(v)
    rememberOpen(node.path, v)
  }
  const [children, setChildren] = useState<TreeNode[] | null>(null)

  // after a rescan an open node refetches in place: dropping its children first would
  // fold the rows away for a moment and lose the scroll position
  useEffect(() => {
    if (!reloadKey) return
    if (open && expandable) api.tree(node.path).then(setChildren).catch(() => {})
    else setChildren(null)
  }, [reloadKey]) // only on a new rescan

  // reveal or collapse changed openPaths: follow it (not on mount: the initial state
  // already came from openPaths, and roots may be open before anything is saved)
  useEffect(() => {
    if (expandable && syncTick) setOpenState(openPaths.has(node.path))
  }, [syncTick, expandable, node.path])

  useEffect(() => {
    if (open && expandable && children === null) {
      api.tree(node.path).then(setChildren).catch(() => setChildren([]))
    }
  }, [open, expandable, children, node.path])

  const selected = !!selectedPath && node.path === selectedPath

  return (
    <>
      <div
        className={'tree-row' + (expandable ? ' expandable' : '') + (selected ?' bg-body-secondary fw-bold' : ' text-body-secondary')}
        data-path={node.path}
        style={{ paddingLeft: 6 + depth * 16 }}
        // a single click anywhere on a folder row toggles it: the arrow alone is too small a target
        onClick={() => expandable && setOpen(!open)}
        title={node.kind === 'track' ? node.name : node.path}
      >
        <span className="tree-toggle text-body-secondary">{expandable ? (open ? '▾' : '▸') : ''}</span>
        {node.kind === 'track' ? (
          <span className="tree-num text-body-secondary">{node.number > 0 ? node.number : ''}</span>
        ) : (
          <span className="tree-icon text-body-secondary"><NodeIcon kind={node.kind} /></span>
        )}
        <span className="tree-name text-truncate">{node.name}</span>
        {/* the button appears on hover, right after the label */}
        <Button
          size="sm" variant="secondary" className="hover-btn"
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
        <Level nodes={children} depth={depth + 1} selectedPath={selectedPath} onMakePlaylist={onMakePlaylist} reloadKey={reloadKey} syncTick={syncTick} />
      )}
    </>
  )
}
