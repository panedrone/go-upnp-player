import { useEffect, useState } from 'react'
import { Button, Card, Collapse, Form, Stack } from 'react-bootstrap'
import { api } from './api'
import { ChevronIcon } from './icons'

const openKey = 'go-upnp-player.folders-open'

interface Props {
  scanning: boolean
  tracks: number
}

// Library folders: a card on the main page.
export default function Settings({ scanning, tracks }: Props) {
  const [text, setText] = useState('')
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  // whether the card body is shown: a per-browser convenience, as for the equalizer
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
    api.settings().then((s) => setText(s.roots.join('\n'))).catch((e) => setMsg({ ok: false, text: String(e) }))
  }, [])

  const act = async (p: Promise<void>, ok: string) => {
    try {
      await p
      setMsg({ ok: true, text: ok })
    } catch (e) {
      setMsg({ ok: false, text: e instanceof Error ? e.message : String(e) })
    }
  }

  const roots = () => text.split('\n').map((s) => s.trim()).filter(Boolean)

  return (
    <Card>
      <Card.Header className="d-flex align-items-center">
        <span className="me-auto">Library folders</span>
        <small className="text-body-secondary">{scanning ? 'scanning…' : `${tracks} tracks`}</small>
        <Button
          size="sm" variant="outline-secondary" className="head-btn ms-2"
          title={open ? 'Hide the library folders' : 'Show the library folders'} aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          <ChevronIcon open={open} />
        </Button>
      </Card.Header>
      <Collapse in={open}>
        <div>
          <Card.Body>
            <Form.Control
              as="textarea" rows={3} size="sm" spellCheck={false} className="font-monospace"
              value={text} onChange={(e) => setText(e.target.value)} placeholder={'D:\\Music'}
            />
            <Form.Text>One folder per line.</Form.Text>
            <Stack direction="horizontal" gap={2} className="mt-2">
              <Button size="sm" variant="secondary" onClick={() => act(api.saveSettings(roots()), 'Saved, rescanning.')}>Save</Button>
              <Button size="sm" variant="outline-secondary" onClick={() => act(api.rescan(), 'Rescanning.')} disabled={scanning}>Rescan</Button>
              {msg && <small className={'ms-auto ' + (msg.ok ? 'text-success' : 'text-danger')}>{msg.text}</small>}
            </Stack>
          </Card.Body>
        </div>
      </Collapse>
    </Card>
  )
}
