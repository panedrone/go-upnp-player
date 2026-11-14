import { useMemo } from 'react'
import { Badge, Button, Image, Spinner, Table } from 'react-bootstrap'
import { artUrl, fmtTime, Track } from './api'
import { PlayIcon } from './icons'

interface Props {
  tracks: Track[]
  playingId: string | null
  pendingIndex: number | null // the track whose start is still in progress
  onPlay: (index: number) => void
  selectedId: string | null // the row the properties panel shows
  onSelect: (id: string) => void
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
export default function Playlist({ tracks, playingId, pendingIndex, onPlay, selectedId, onSelect }: Props) {
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
            <div className="album-head d-flex align-items-center px-2 py-2" title={g.key}>
              <Image
                src={artUrl(g.items[0].t.id)} alt="" rounded loading="lazy" className="album-thumb"
                onError={(e) => (e.target as HTMLImageElement).classList.add('d-none')}
              />
              <div className="album-text min-w-0">
                <div className="fw-semibold text-truncate">{g.album}</div>
                <small className="text-body-secondary text-truncate d-block">
                  {[g.artist, g.date].filter(Boolean).join(' · ')}
                </small>
              </div>
              <Badge bg="primary-subtle" text="primary-emphasis" className="ms-auto flex-shrink-0">
                {fmtTime(total)}
              </Badge>
            </div>
            <Table size="sm" hover borderless className="mb-2 track-table">
              <tbody>
                {g.items.map(({ t, i }) => {
                  const playing = t.id === playingId
                  return (
                    <tr
                      key={t.id}
                      className={playing ? 'table-active fw-bold' : t.id === selectedId ? 'table-active' : ''}
                      onClick={() => onSelect(t.id)}
                    >
                      <td className="col-num text-body-secondary text-nowrap">
                        {/* left of the number: the ▶ mark of the playing track, and on row hover the
                            Play button in its place */}
                        <div className="num-cell">
                          <span className="play-slot">
                            {playing && <span className="play-mark" title="Playing"><PlayIcon /></span>}
                            <Button
                              size="sm" variant="secondary" className="hover-btn icon-btn" title="Play"
                              onClick={(e) => {
                                e.stopPropagation()
                                onPlay(i)
                              }}
                            >
                              <PlayIcon />
                            </Button>
                          </span>
                          {t.number > 0 ? t.number : ''}
                        </div>
                      </td>
                      <td>
                        <div className="row-label">
                          <span className={'text-truncate' + (playing ? '' : ' text-body-secondary')}>
                            {t.title}
                            {t.artist && t.artist !== g.artist ? <small className="text-body-secondary"> — {t.artist}</small> : null}
                          </span>
                          {/* while the start is under way, right after the title */}
                          {pendingIndex === i && <Spinner size="sm" className="ms-2 flex-shrink-0 text-body-secondary" />}
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
