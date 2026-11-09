import { useEffect, useState } from 'react'
import { Button, ButtonGroup, Card, Form } from 'react-bootstrap'
import { api, EQ } from './api'

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
  // harder: more low end, the 250–500 Hz "mud" cut, guitar attack at 2–4 kHz pushed forward
  Hard: curve([[55, 6], [110, 5], [220, 1], [400, -3], [800, -1], [2000, 3], [3500, 4], [7000, 3], [14000, 5]]),
  // soft: a little warmth at the bottom, harsh 3–8 kHz and the very top tamed
  Soft: curve([[55, 2], [220, 1], [700, 0], [2000, -1], [4000, -3], [8000, -4], [14000, -5]]),
}

// 1245 → 1.2k, 4978 → 5k, 19912 → 20k
const fmtBand = (hz: number) => {
  if (hz < 1000) return String(hz)
  const k = hz / 1000
  return (k < 10 ? (Math.round(k * 10) / 10).toString() : Math.round(k).toString()) + 'k'
}

interface Props {
  onError: (msg: string) => void
}

// The equalizer is applied on our side, in the WAV stream the renderer pulls.
export default function Equalizer({ onError }: Props) {
  const [eq, setEq] = useState<EQ | null>(null)

  useEffect(() => {
    api.eq().then(setEq).catch((e) => onError(String(e)))
  }, [onError])

  if (!eq) return null

  const save = (next: EQ) => {
    setEq(next)
    api.saveEq(next.enabled, next.gains).catch((e) => onError(e instanceof Error ? e.message : String(e)))
  }
  const setGain = (i: number, v: number) => setEq({ ...eq, gains: eq.gains.map((g, j) => (j === i ? v : g)) })

  return (
    <Card>
      <Card.Header className="d-flex align-items-center">
        <span className="me-auto">Equalizer <small className="text-body-secondary">±{eq.maxGain} dB</small></span>
        <Form.Check
          type="switch" id="eq-enabled" label={eq.enabled ? 'On' : 'Off'}
          checked={eq.enabled} onChange={(e) => save({ ...eq, enabled: e.target.checked })}
        />
      </Card.Header>
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
        </ButtonGroup>
      </Card.Body>
    </Card>
  )
}
