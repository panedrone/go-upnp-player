import { useEffect, useState } from 'react'
import { Table } from 'react-bootstrap'
import { api, TrackProps } from './api'

// 201823944 -> "201 823 944"
const grouped = (n: number) => n.toLocaleString('en-US').replace(/,/g, ' ')

// 4576.507 -> "1:16:16.507", as foobar2000 shows a length
function fmtPrecise(sec: number): string {
  const ms = Math.round(sec * 1000)
  const h = Math.floor(ms / 3600000)
  const m = Math.floor((ms % 3600000) / 60000)
  const s = Math.floor((ms % 60000) / 1000)
  const frac = String(ms % 1000).padStart(3, '0')
  const ss = String(s).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}.${frac}` : `${m}:${ss}.${frac}`
}

// Properties of one playlist track: its tags and what the FLAC header says.
export default function TrackProperties({ id }: { id: string | null }) {
  const [props, setProps] = useState<TrackProps | null>(null)
  const [err, setErr] = useState('')

  // No spinner while the next track's properties load: the answer comes in a moment, and a
  // spinner flashed in the middle of the card on every click. The previous track's stay
  // until then.
  useEffect(() => {
    if (!id) {
      setProps(null)
      setErr('')
      return
    }
    let live = true
    api
      .properties(id)
      .then((p) => {
        if (!live) return
        setProps(p)
        setErr('')
      })
      .catch((e) => live && setErr(String(e?.message ?? e)))
    return () => {
      live = false
    }
  }, [id])

  if (!id) return <div className="p-3 text-body-secondary">Click a track in the playlist to see its properties.</div>
  if (err) return <div className="p-3 text-danger">{err}</div>
  if (!props) return null

  const metadata: [string, string][] = [
    ['Artist name', props.artist],
    ['Track title', props.title],
    ['Album title', props.album],
    ['Date', props.date],
    ['Genre', props.genre],
    ['Track number', props.number > 0 ? String(props.number).padStart(2, '0') : ''],
    ['Comment', props.comment],
  ]
  const general: [string, string][] = [
    ['Duration', `${fmtPrecise(props.duration)} (${grouped(props.samples)} samples)`],
    ['Sample rate', `${props.sampleRate} Hz`],
    ['Channels', String(props.channels)],
    ['Bits per sample', String(props.bits)],
    ['Bitrate', props.bitrateKbps > 0 ? `${props.bitrateKbps} kbps` : ''],
    ['Codec', props.codec],
    ['Encoding', props.encoding],
    ['Tool', props.tool],
    ['Embedded cuesheet', props.embeddedCue ? 'yes' : 'no'],
    ['Audio MD5', props.md5],
  ]
  return (
    <div className="props px-2 pb-2">
      <PropsSection title="Metadata" rows={metadata} />
      <PropsSection title="General" rows={general} />
    </div>
  )
}

function PropsSection({ title, rows }: { title: string; rows: [string, string][] }) {
  return (
    <>
      <div className="props-head fw-semibold pt-2 pb-1">{title}</div>
      <Table size="sm" borderless className="props-table mb-0">
        <tbody>
          {rows
            .filter(([, v]) => v !== '')
            .map(([k, v]) => (
              <tr key={k}>
                <td className="props-key text-body-secondary text-nowrap">{k}</td>
                {/* values as quiet as the playlist rows: bright text is for the playing row only */}
                <td className="props-val text-body-secondary">{v}</td>
              </tr>
            ))}
        </tbody>
      </Table>
    </>
  )
}
