// Text beside a slider or a button is centred by its line box, but the capitals and digits are
// not in the middle of that box: how far off they are depends on the font. Segoe UI (Windows)
// puts them about 1 px low, the Linux fonts (Ubuntu Sans, Liberation Mono) about 2 px high. A fixed
// nudge suits one system and doubles the error on the other, so the offset is measured from the
// font in use and handed to CSS as --cap-shift-sans and --cap-shift-mono (in em; styles.css,
// .cap-mid).

// how far to move text of this font family so that its capitals sit on the middle of its line,
// in em: negative is up
export function capShiftEm(family: string): number {
  const ctx = document.createElement('canvas').getContext('2d')
  if (!ctx) return 0
  ctx.font = `100px ${family}`
  const m = ctx.measureText('H')
  const a = m.fontBoundingBoxAscent
  const d = m.fontBoundingBoxDescent
  const cap = m.actualBoundingBoxAscent
  if (!a || !cap) return 0 // a browser without these metrics: leave the text where it is
  // the line box centres the font's ascent + descent; the capitals' middle is cap/2 above the
  // baseline, which is (a - d) / 2 below the line's middle
  const below = (a - d) / 2 - cap / 2
  return -below / 100
}

function familyOf(cls: string): string {
  const el = document.createElement('span')
  el.className = cls
  document.body.appendChild(el)
  const f = getComputedStyle(el).fontFamily
  el.remove()
  return f
}

export function applyCapShift() {
  const set = () => {
    const root = document.documentElement.style
    root.setProperty('--cap-shift-sans', capShiftEm(familyOf('')).toFixed(4) + 'em')
    root.setProperty('--cap-shift-mono', capShiftEm(familyOf('font-monospace')).toFixed(4) + 'em')
  }
  set()
  // a web or late-loading font changes the metrics: measure again once fonts are in
  document.fonts?.ready.then(set).catch(() => {})
}
