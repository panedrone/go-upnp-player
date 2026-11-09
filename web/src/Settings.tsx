import { useEffect, useState } from 'react'
import { Button, Card, Form, Stack } from 'react-bootstrap'
import { api } from './api'

interface Props {
  scanning: boolean
  tracks: number
}

// Library folders: a card on the main page.
export default function Settings({ scanning, tracks }: Props) {
  const [text, setText] = useState('')
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)

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
      </Card.Header>
      <Card.Body>
        <Form.Control
          as="textarea" rows={3} size="sm" spellCheck={false} className="font-monospace"
          value={text} onChange={(e) => setText(e.target.value)} placeholder={'D:\\Music'}
        />
        <Form.Text>One folder per line.</Form.Text>
        <Stack direction="horizontal" gap={2} className="mt-2">
          <Button size="sm" variant="primary" onClick={() => act(api.saveSettings(roots()), 'Saved, rescanning.')}>Save</Button>
          <Button size="sm" variant="outline-secondary" onClick={() => act(api.rescan(), 'Rescanning.')} disabled={scanning}>Rescan</Button>
          {msg && <small className={'ms-auto ' + (msg.ok ? 'text-success' : 'text-danger')}>{msg.text}</small>}
        </Stack>
      </Card.Body>
    </Card>
  )
}
