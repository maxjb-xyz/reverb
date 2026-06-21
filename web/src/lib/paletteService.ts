import type { RGB } from './palette'

export type ComputeFn = (coverUrl: string) => Promise<RGB>

const cache = new Map<string, RGB>()
const inflight = new Map<string, Promise<RGB>>()

// testComputeFn, when set, replaces the worker entirely (tests + SSR safety). It is
// null in production so the real worker is used.
let testComputeFn: ComputeFn | null = null

// worker is created lazily on first real use so importing this module never spins a
// Worker (which would break jsdom and SSR).
let worker: Worker | null = null
let nextId = 0
const pending = new Map<string, { resolve: (v: RGB) => void; reject: (e: Error) => void }>()

function ensureWorker(): Worker {
  if (worker) return worker
  worker = new Worker(new URL('./paletteWorker.ts', import.meta.url), { type: 'module' })
  worker.onmessage = (e: MessageEvent<{ id: string; rgb?: RGB; error?: string }>) => {
    const { id, rgb, error } = e.data
    const p = pending.get(id)
    if (!p) return
    pending.delete(id)
    if (rgb) p.resolve(rgb)
    else p.reject(new Error(error ?? 'palette failed'))
  }
  return worker
}

function computeViaWorker(coverUrl: string): Promise<RGB> {
  const id = String(nextId++)
  const w = ensureWorker()
  return new Promise<RGB>((resolve, reject) => {
    pending.set(id, { resolve, reject })
    w.postMessage({ id, coverUrl })
  })
}

// getPalette resolves the dominant color for a cover URL, computing it exactly once
// per URL (cache + in-flight de-dup). Uses the injected test fn when present.
export function getPalette(coverUrl: string): Promise<RGB> {
  const cached = cache.get(coverUrl)
  if (cached) return Promise.resolve(cached)
  const existing = inflight.get(coverUrl)
  if (existing) return existing

  const compute = testComputeFn ?? computeViaWorker
  const promise = compute(coverUrl)
    .then((rgb) => {
      cache.set(coverUrl, rgb)
      inflight.delete(coverUrl)
      return rgb
    })
    .catch((err) => {
      inflight.delete(coverUrl)
      throw err
    })
  inflight.set(coverUrl, promise)
  return promise
}

// __setComputeFnForTests swaps the compute path (tests inject a synchronous fake so
// no real Worker/OffscreenCanvas is needed). Pass null to restore production behavior.
export function __setComputeFnForTests(fn: ComputeFn | null): void {
  testComputeFn = fn
}

// __resetForTests clears the cache, in-flight map, and any installed fake.
export function __resetForTests(): void {
  cache.clear()
  inflight.clear()
  pending.clear()
  testComputeFn = null
}
