import { useEffect, useState } from 'react'
import { Button, Spinner } from 'react-bootstrap'
import { api, fmtTime, TreeNode } from './api'

// Expanded tree nodes live in the browser localStorage, so they survive a page
// reload and an app restart. Storage may be unavailable (private window,
// site data blocked); then we just work without persistence.
const OPEN_KEY = 'upnp-player.tree.open'

// hadSavedState: the key already exists (even if empty: the user collapsed everything)
const [openPaths, hadSavedState] = ((): [Set<string>, boolean] => {
  try {
    const raw = localStorage.getItem(OPEN_KEY)
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

interface Props {
  playingId: string | null
  // arrow button: build a playlist from the node (folder, cue, file, track)
  onMakePlaylist: (node: TreeNode) => void
  reloadKey: number
}

// Tree following the folder structure: only what has something playable. A cue expands
// into its tracks, track files are leaves. Nothing plays from the tree: the arrow button
// on a row builds a playlist from the node, and the playlist is what plays.
export default function Tree({ playingId, onMakePlaylist, reloadKey }: Props) {
  const [roots, setRoots] = useState<TreeNode[] | null>(null)
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
      <Level nodes={roots} depth={0} playingId={playingId} onMakePlaylist={onMakePlaylist} reloadKey={reloadKey} rootLevel />
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

const icons: Record<TreeNode['kind'], string> = { dir: '📁', cue: '💿', file: '♪', track: '' }

function TreeItem({ node, depth, playingId, onMakePlaylist, initiallyOpen, reloadKey }: ItemProps) {
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
        {node.kind === 'track' ? (
          <span className="tree-num text-body-secondary">{playing ? '▶' : node.number > 0 ? node.number : ''}</span>
        ) : (
          <span className="tree-icon">{playing ? '▶' : icons[node.kind]}</span>
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
        <Level nodes={children} depth={depth + 1} playingId={playingId} onMakePlaylist={onMakePlaylist} reloadKey={reloadKey} />
      )}
    </>
  )
}
