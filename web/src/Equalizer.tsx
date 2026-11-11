import { useEffect, useRef, useState } from 'react'
import { Button, ButtonGroup, Card, Collapse, Form } from 'react-bootstrap'
import { api, EQ } from './api'
import { ChevronIcon } from './icons'

// Presets are frequency curves, not arrays tied to a specific band grid:
// the grid comes from the backend and may change.
const oct = (hz: number, ref: number) => Math.log2(hz / ref)
const clampDb = (v: number) => Math.round(Math.max(-12, Math.min(12, v)))

// curve: a curve through anchor points [Hz, dB], linear in octaves between them,
// beyond the ends it holds the value of the outermost point.
const curve = (points: [number, number][]) => (hz: number) => {
  if (hz <= points[0][0]) return clampDb(points[0][1])
  for (let i = 1; i < points.length; i++) {
    const [f1, g1] = points[i]
    if (hz <= f1) {
      const [f0, g0] = points[i - 1]
      return clampDb(g0 + ((g1 - g0) * oct(hz, f0)) / oct(f1, f0))
    }
  }
  return clampDb(points[points.length - 1][1])
}

const presets: Record<string, (hz: number) => number> = {
  Flat: () => 0,
  Bass: (hz) => clampDb(hz < 250 ? 8 - 2.5 * Math.max(0, oct(hz, 55)) : 0),
  Treble: (hz) => clampDb(hz > 2000 ? 2.5 * oct(hz, 2000) : 0),
  Loudness: (hz) => clampDb(hz < 300 ? 7 - 2.5 * Math.max(0, oct(hz, 55)) : hz > 3000 ? 2 * oct(hz, 3000) : 0),
  Vocal: (hz) => clampDb(4 - 2.5 * Math.abs(oct(hz, 1500))),
  // "smiley" (V): moderate bass, a mid dip around 1 kHz and strongly boosted
  // highs; the right wing is higher than the left (per a live listening test 2026-11-09:
  // a symmetric V gave "too much bass, too little treble")
  Rock: curve([[55, 4], [110, 4], [250, 1], [600, -2], [1000, -3], [2000, 0], [3500, 4], [6000, 7], [10000, 9], [14000, 10]]),
}

// foobar2000 .feq: one integer gain in dB per line, one line per band, CRLF.
// foobar's 18 bands (55 Hz…20 kHz, half an octave apart, ±20 dB) are the same grid as ours,
// so a file maps onto the sliders index by index. Some files lack the final line break.
const parseFeq = (text: string, bands: number, maxGain: number): number[] => {
  const lines = text.split(/\r?\n/).map((s) => s.trim()).filter((s) => s !== '')
  if (lines.length !== bands) throw new Error(`not an equalizer preset: expected ${bands} values, got ${lines.length}`)
  return lines.map((s) => {
    const v = Number(s)
    if (!Number.isFinite(v)) throw new Error(`not an equalizer preset: bad value "${s}"`)
    return Math.round(Math.max(-maxGain, Math.min(maxGain, v)))
  })
}

const formatFeq = (gains: number[]) => gains.map((g) => String(Math.round(g)) + '\r\n').join('')

// saveFile: the browser's "Save as" where there is one (Chromium), otherwise a plain download.
const saveFile = async (name: string, text: string) => {
  const w = window as unknown as {
    showSaveFilePicker?: (o: unknown) => Promise<{ createWritable: () => Promise<{ write: (d: Blob) => Promise<void>; close: () => Promise<void> }> }>
  }
  const blob = new Blob([text], { type: 'application/octet-stream' })
  if (w.showSaveFilePicker) {
    try {
      const h = await w.showSaveFilePicker({ suggestedName: name, types: [{ description: 'foobar2000 EQ preset', accept: { 'application/octet-stream': ['.feq'] } }] })
      const f = await h.createWritable()
      await f.write(blob)
      await f.close()
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') return // cancelled by the user
      throw e
    }
    return
  }
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = name
  a.click()
  URL.revokeObjectURL(a.href)
}

// 1245 → 1.2k, 4978 → 5k, 19912 → 20k
const fmtBand = (hz: number) => {
  if (hz < 1000) return String(hz)
  const k = hz / 1000
  return (k < 10 ? (Math.round(k * 10) / 10).toString() : Math.round(k).toString()) + 'k'
}

const openKey = 'go-upnp-player.eq-open'

interface Props {
  onError: (msg: string) => void
}

// The equalizer is applied on our side, in the WAV stream the renderer pulls.
export default function Equalizer({ onError }: Props) {
  const [eq, setEq] = useState<EQ | null>(null)
  // the name of the last loaded preset, offered back when saving
  const [presetName, setPresetName] = useState('equalizer.feq')
  const fileInput = useRef<HTMLInputElement>(null)
  // whether the panel is shown: a per-browser convenience, like the theme; hiding it
  // changes nothing in the sound
  const [open, setOpen] = useState(() => {
    try {
      return localStorage.getItem(openKey) !== '0'
    } catch {
      return true // no storage: the panel is just not remembered
    }
  })
  useEffect(() => {
    try {
      localStorage.setItem(openKey, open ? '1' : '0')
    } catch {
      // no storage: the panel is just not remembered
    }
  }, [open])

  useEffect(() => {
    api.eq().then(setEq).catch((e) => onError(String(e)))
  }, [onError])

  if (!eq) return null

  const save = (next: EQ) => {
    setEq(next)
    api.saveEq(next.enabled, next.gains).catch((e) => onError(e instanceof Error ? e.message : String(e)))
  }
  const setGain = (i: number, v: number) => setEq({ ...eq, gains: eq.gains.map((g, j) => (j === i ? v : g)) })
  const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e))

  const loadFeq = async (file: File) => {
    try {
      const gains = parseFeq(await file.text(), eq.bands.length, eq.maxGain)
      setPresetName(file.name)
      save({ ...eq, gains })
    } catch (e) {
      onError(errMsg(e))
    }
  }

  return (
    <Card>
      <Card.Header className="d-flex align-items-center">
        <span className="me-auto">Equalizer <small className="text-body-secondary">±{eq.maxGain} dB</small></span>
        <Form.Check
          type="switch" id="eq-enabled" label={eq.enabled ? 'On' : 'Off'}
          checked={eq.enabled} onChange={(e) => save({ ...eq, enabled: e.target.checked })}
        />
        <Button
          size="sm" variant="outline-secondary" className="head-btn ms-2"
          title={open ? 'Hide the equalizer' : 'Show the equalizer'} aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          <ChevronIcon open={open} />
        </Button>
      </Card.Header>
      <Collapse in={open}>
        <div>
          <Card.Body className={eq.enabled ? '' : 'opacity-50'}>
            <div className="eq-bands">
              {eq.bands.map((hz, i) => (
                <div className="eq-band" key={hz}>
                  <small className="eq-gain">{eq.gains[i] === 0 ? '·' : (eq.gains[i] > 0 ? '+' : '') + eq.gains[i]}</small>
                  <Form.Range
                    className="eq-slider" min={-eq.maxGain} max={eq.maxGain} step={1}
                    value={eq.gains[i]} disabled={!eq.enabled}
                    onChange={(e) => setGain(i, Number(e.target.value))}
                    onPointerUp={() => save(eq)}
                    onKeyUp={() => save(eq)}
                    title={`${hz} Hz: ${eq.gains[i] > 0 ? '+' : ''}${eq.gains[i]} dB`}
                  />
                  <small className="text-body-secondary eq-hz">{fmtBand(hz)}</small>
                </div>
              ))}
            </div>
            <ButtonGroup size="sm" className="mt-3 w-100 flex-wrap">
              {Object.entries(presets).map(([name, gains]) => (
                <Button key={name} variant="outline-secondary" disabled={!eq.enabled} onClick={() => save({ ...eq, gains: eq.bands.map(gains) })}>
                  {name}
                </Button>
              ))}
              <Button variant="outline-secondary" disabled={!eq.enabled} title="Load a foobar2000 .feq preset" onClick={() => fileInput.current?.click()}>
                Load…
              </Button>
              <Button variant="outline-secondary" disabled={!eq.enabled} title="Save as a foobar2000 .feq preset" onClick={() => saveFile(presetName, formatFeq(eq.gains)).catch((e) => onError(errMsg(e)))}>
                Save…
              </Button>
            </ButtonGroup>
            <input
              ref={fileInput} type="file" accept=".feq" hidden
              onChange={(e) => {
                const f = e.target.files?.[0]
                e.target.value = '' // the same file can be picked again
                if (f) loadFeq(f)
              }}
            />
          </Card.Body>
        </div>
      </Collapse>
    </Card>
  )
}
