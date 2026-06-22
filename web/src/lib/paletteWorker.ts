// Web Worker: extracts the dominant color of a cover image OFF the main thread, so
// the player-bar / background transition never drops a frame. It is a thin wrapper
// around the pure helpers in ./palette — no extraction math lives here.
//
// NOTE: this file is never imported by tests (jsdom has no Worker/OffscreenCanvas);
// the palette SERVICE is tested with an injected fake computeFn instead.
import { dominantColorFromPixels, type RGB } from './palette'

// SAMPLE_SIZE downscales the cover before sampling: 32×32 = 1024 pixels is plenty
// for an ambient dominant color and keeps the work sub-millisecond.
const SAMPLE_SIZE = 32

// credentialsFor returns 'include' for same-origin URLs (our auth-gated /api/... proxy)
// and 'omit' for cross-origin URLs (Spotify CDN, etc. which return
// Access-Control-Allow-Origin: * but do NOT support credentialed requests).
// A URL is same-origin if it is relative (starts with '/') — which covers all
// library covers (/api/v1/cover/...) — or if its parsed origin matches self.location.
// Everything else (absolute https://... external URLs) gets 'omit' so anonymous CORS works.
export function credentialsFor(url: string): RequestCredentials {
  if (url.startsWith('/')) return 'include'
  try {
    const workerOrigin = (typeof self !== 'undefined' && self.location?.origin) || ''
    return new URL(url).origin === workerOrigin ? 'include' : 'omit'
  } catch {
    return 'omit'
  }
}

interface Request {
  id: string
  coverUrl: string
}

async function extract(coverUrl: string): Promise<RGB> {
  const res = await fetch(coverUrl, { credentials: credentialsFor(coverUrl) })
  const blob = await res.blob()
  const bitmap = await createImageBitmap(blob)
  const canvas = new OffscreenCanvas(SAMPLE_SIZE, SAMPLE_SIZE)
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('no 2d context')
  ctx.drawImage(bitmap, 0, 0, SAMPLE_SIZE, SAMPLE_SIZE)
  bitmap.close()
  const { data } = ctx.getImageData(0, 0, SAMPLE_SIZE, SAMPLE_SIZE)
  return dominantColorFromPixels(data)
}

self.onmessage = (e: MessageEvent<Request>) => {
  const { id, coverUrl } = e.data
  extract(coverUrl)
    .then((rgb) => self.postMessage({ id, rgb }))
    .catch((err: unknown) => self.postMessage({ id, error: err instanceof Error ? err.message : 'palette failed' }))
}
