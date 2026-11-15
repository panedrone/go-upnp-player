import { useEffect, useState } from 'react'
import { Button, ButtonGroup, Card, Collapse } from 'react-bootstrap'
import { api, DSDSettings } from './api'
import { ChevronIcon } from './icons'

const openKey = 'go-upnp-player.dsd-open'

interface Props {
  onError: (msg: string) => void
}

// DSD: how .dsf/.dff files become PCM on their way to the streamer (ADR-028). The settings live
// on the server in their own file; a click applies at once.
export default function DsdCard({ onError }: Props) {
  const [s, setS] = useState<DSDSettings | null>(null)
  // whether the card body is shown: a per-browser convenience, as for the other cards
  const [open, setOpen] = useState(() => {
    try {
      return localStorage.getItem(openKey) !== '0'
    } catch {
      return true // no storage: the card is just not remembered
    }
  })
  useEffect(() => {
    try {
      localStorage.setItem(openKey, open ? '1' : '0')
    } catch {
      // no storage: the card is just not remembered
    }
  }, [open])

  useEffect(() => {
    api.dsd().then(setS).catch((e) => onError(String(e)))
  }, [onError])

  const save = (next: DSDSettings) => {
    const prev = s
    setS(next)
    api.saveDsd(next.rate, next.gainDb).catch((e) => {
      setS(prev)
      onError(e instanceof Error ? e.message : String(e))
    })
  }

  return (
    <Card>
      <Card.Header className="d-flex align-items-center gap-2">
        <span className="d-flex align-items-baseline gap-2 me-auto">
          <span>DSD</span>
          <small className="text-body-secondary">→ PCM</small>
        </span>
        <Button
          size="sm" variant="outline-secondary" className="head-btn"
          title={open ? 'Hide the DSD settings' : 'Show the DSD settings'} aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          <ChevronIcon open={open} />
        </Button>
      </Card.Header>
      <Collapse in={open}>
        <div>
          <Card.Body>
            {s && (
              <div className="dsd-grid">
                {/* the choices are button groups like the transport's: one height, outlined, the
                    chosen one pressed; the unit once after them */}
                <span className="text-body-secondary">Rate</span>
                <span className="dsd-choice">
                  <ButtonGroup>
                    {s.rates.map((r) => (
                      <Button
                        key={r} size="sm" variant="outline-secondary" className="dsd-btn" active={r === s.rate} title={`${r} Hz`}
                        aria-pressed={r === s.rate} onClick={() => r !== s.rate && save({ ...s, rate: r })}
                      >
                        {/* whole kHz on the buttons: 44, 88, 176; the exact rate is in the title */}
                        {Math.floor(r / 1000)}
                      </Button>
                    ))}
                  </ButtonGroup>
                  <span className="text-body-secondary">kHz</span>
                </span>
                <span className="text-body-secondary">Gain</span>
                <span className="dsd-choice">
                  <ButtonGroup>
                    {s.gains.map((g) => (
                      <Button
                        key={g} size="sm" variant="outline-secondary" className="dsd-btn" active={g === s.gainDb}
                        aria-pressed={g === s.gainDb} onClick={() => g !== s.gainDb && save({ ...s, gainDb: g })}
                      >
                        {g > 0 ? `+${g}` : g}
                      </Button>
                    ))}
                  </ButtonGroup>
                  <span className="text-body-secondary">dB</span>
                </span>
              </div>
            )}
            <div className="form-text mt-2">
              .dsf and .dff files play converted to 24-bit PCM. +6 dB brings SACD's full level to
              full scale and may clip loud masters; 0 dB never clips. A change restarts a playing
              DSD track where it is.
            </div>
          </Card.Body>
        </div>
      </Collapse>
    </Card>
  )
}
