import { useMemo, useState } from 'react'
import { Badge, Button, Image, Table } from 'react-bootstrap'
import { artUrl, fmtTime, Track } from './api'

interface Props {
  tracks: Track[]
  playingId: string | null
  onPlay: (index: number) => void
}

interface Group {
  key: string
  artist: string
  album: string
  date: string
  first: number
  items: { t: Track; i: number }[]
}

// Track list grouped by album (cue or folder).
export default function Playlist({ tracks, playingId, onPlay }: Props) {
  const [sel, setSel] = useState<string | null>(null)
  const groups = useMemo(() => {
    const out: Group[] = []
    tracks.forEach((t, i) => {
      const key = t.fromCue ? t.leaf : t.leaf.replace(/[\\/][^\\/]*$/, '')
      let g = out[out.length - 1]
      if (!g || g.key !== key) {
        g = { key, artist: t.artist, album: t.album || key.replace(/^.*[\\/]/, ''), date: t.date, first: i, items: [] }
        out.push(g)
      }
      g.items.push({ t, i })
    })
    return out
  }, [tracks])

  if (tracks.length === 0) {
    return <div className="p-4 text-center text-body-secondary">Playlist is empty. Press → next to a folder, a cue sheet or a track in the library. Hover a track here and press ▶ to play it.</div>
  }
  return (
    <div className="playlist">
      {groups.map((g) => {
        const total = g.items.reduce((s, x) => s + x.t.duration, 0)
        return (
          <section key={g.key + g.first}>
            <div className="album-head d-flex align-items-center gap-2 px-2 py-2" title={g.key}>
              <Image
                src={artUrl(g.items[0].t.id)} alt="" rounded loading="lazy" className="album-thumb"
                onError={(e) => ((e.target as HTMLImageElement).style.visibility = 'hidden')}
              />
              <div className="min-w-0">
                <div className="fw-semibold text-truncate">{g.album}</div>
                <small className="text-body-secondary text-truncate d-block">
                  {[g.artist, g.date].filter(Boolean).join(' · ')}
                </small>
              </div>
              <Badge bg="primary-subtle" text="primary-emphasis" className="ms-auto">
                {g.items.length} · {fmtTime(total)}
              </Badge>
            </div>
            <Table size="sm" hover borderless className="mb-2 track-table">
              <tbody>
                {g.items.map(({ t, i }) => {
                  const playing = t.id === playingId
                  return (
                    <tr
                      key={t.id}
                      className={playing ? 'table-active fw-bold' : t.id === sel ? 'table-active' : ''}
                      onClick={() => setSel(t.id)}
                    >
                      <td className="col-num text-end text-body-secondary">
                        {t.number > 0 ? t.number : ''}
                      </td>
                      <td>
                        {/* the button appears on row hover, right after the title */}
                        <div className="row-label">
                          <span className={'text-truncate' + (playing ? '' : ' text-body-secondary')}>
                            {t.title}
                            {t.artist && t.artist !== g.artist ? <small className="text-body-secondary"> — {t.artist}</small> : null}
                          </span>
                          <Button
                            size="sm" variant="primary" className="hover-btn" title="Play"
                            onClick={(e) => {
                              e.stopPropagation()
                              onPlay(i)
                            }}
                          >
                            ▶
                          </Button>
                        </div>
                      </td>
                      <td className="col-dur text-end text-body-secondary">{fmtTime(t.duration)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </Table>
          </section>
        )
      })}
    </div>
  )
}
