// Icons of the card header buttons: drawn, one size and one stroke, like the top bar's.
// Font symbols (⟳ ▾ ▸) came out in different sizes and off centre (2026-11-11).

export function RefreshIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
      <path d="M13.5 8a5.5 5.5 0 1 1-1.6-3.9" />
      <path d="M12.3 1.3v3.2H9.1" />
    </svg>
  )
}

// a card shown (pointing down) or hidden (pointing right)
export function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={open ? 'M4 6l4 4 4-4' : 'M6 4l4 4-4 4'} />
    </svg>
  )
}

// the row button that makes a playlist of a tree node or a search hit: an arrow drawn
// like the other icons; the "→" character sat low and thin (2026-11-13)
export function ToPlaylistIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M2.5 8h10.5M9 3.5L13.5 8 9 12.5" />
    </svg>
  )
}

// the playlist row's Play button: a filled triangle, nudged right so it looks centred
export function PlayIcon() {
  return (
    <svg width="10" height="10" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
      <path d="M4.5 2.2v11.6c0 .5.5.8 1 .5l9-5.8a.6.6 0 0 0 0-1L5.5 1.7c-.5-.3-1 0-1 .5z" />
    </svg>
  )
}

// "Check for updates": a cloud with an arrow down
export function UpdateIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M4.5 11.5H4a3 3 0 0 1-.4-6 4.5 4.5 0 0 1 8.7 1A2.5 2.5 0 0 1 12 11.5h-.5" />
      <path d="M8 7.5v6M5.8 11.3L8 13.5l2.2-2.2" />
    </svg>
  )
}
