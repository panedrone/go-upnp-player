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
