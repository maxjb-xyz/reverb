/** Resample to a fixed bar count, keeping the loudest sample in each bucket so
 *  transients survive. */
export function resamplePeaks(peaks: number[], bars: number): number[] {
  if (peaks.length <= bars) return peaks
  const out: number[] = []
  for (let i = 0; i < bars; i++) {
    const start = Math.floor((i * peaks.length) / bars)
    const end = Math.max(start + 1, Math.floor(((i + 1) * peaks.length) / bars))
    let max = 0
    for (let j = start; j < end; j++) max = Math.max(max, peaks[j])
    out.push(max)
  }
  return out
}
