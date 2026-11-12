/** Up to two initials for an artist avatar ("Halcyon Drift" → "HD"). */
export function initialsOf(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return ''
  const first = Array.from(words[0])[0] ?? ''
  const last = words.length > 1 ? Array.from(words[words.length - 1])[0] ?? '' : ''
  return (first + last).toUpperCase()
}

/** Music servers answer a missing artist photo with a stock silhouette on a
 *  white card, which glares on a dark page. An image that is almost entirely
 *  near-white and colourless is treated as that stock picture. Pure function
 *  over RGBA pixels so it can be tested without a canvas. */
export function looksLikeBlankPlaceholder(data: Uint8ClampedArray): boolean {
  const total = Math.floor(data.length / 4)
  if (total === 0) return false
  let pale = 0
  for (let i = 0; i < total * 4; i += 4) {
    const r = data[i], g = data[i + 1], b = data[i + 2]
    const max = Math.max(r, g, b), min = Math.min(r, g, b)
    if (max - min < 18 && min > 170) pale++
  }
  return pale / total > 0.7
}
