import { describe, it, expect, beforeEach, vi } from 'vitest'
import { getPalette, __setComputeFnForTests, __resetForTests } from './paletteService'
import type { RGB } from './palette'

describe('paletteService', () => {
  beforeEach(() => {
    __resetForTests()
  })

  it('computes via the injected fn and caches per URL (one compute per URL)', async () => {
    const fake = vi.fn<(u: string) => Promise<RGB>>(async () => [10, 20, 30] as RGB)
    __setComputeFnForTests(fake)

    const a1 = await getPalette('/cover/a')
    const a2 = await getPalette('/cover/a')
    const b1 = await getPalette('/cover/b')

    expect(a1).toEqual([10, 20, 30])
    expect(a2).toEqual([10, 20, 30])
    expect(b1).toEqual([10, 20, 30])
    // /cover/a computed once (cached on the 2nd call), /cover/b once → 2 total.
    expect(fake).toHaveBeenCalledTimes(2)
  })

  it('shares one in-flight promise for concurrent identical URLs', async () => {
    let resolveFn: (v: RGB) => void = () => {}
    const fake = vi.fn<(u: string) => Promise<RGB>>(
      () => new Promise<RGB>((res) => { resolveFn = res }),
    )
    __setComputeFnForTests(fake)

    const p1 = getPalette('/cover/x')
    const p2 = getPalette('/cover/x')
    resolveFn([1, 2, 3])
    const [r1, r2] = await Promise.all([p1, p2])

    expect(r1).toEqual([1, 2, 3])
    expect(r2).toEqual([1, 2, 3])
    expect(fake).toHaveBeenCalledTimes(1) // de-duped while in flight
  })

  it('does not construct a real Worker when a test fn is set', async () => {
    // jsdom has no Worker; if the service tried to build one this would throw.
    __setComputeFnForTests(async () => [5, 5, 5] as RGB)
    await expect(getPalette('/cover/safe')).resolves.toEqual([5, 5, 5])
  })
})
