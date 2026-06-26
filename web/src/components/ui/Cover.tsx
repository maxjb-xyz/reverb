import React, { useEffect, useState } from 'react'
import { Icon } from './Icon'
import { Skeleton } from './Skeleton'

interface CoverProps {
  src?: string
  /** Tried once if `src` fails to load. Used for library track covers, where the
   *  per-song artwork may be missing but the album artwork resolves. */
  fallbackSrc?: string
  alt: string
  size?: number | 'full'
  rounded?: 'md' | 'full'
  className?: string
}

export function Cover({ src, fallbackSrc, alt, size = 48, rounded = 'md', className }: CoverProps) {
  const [loaded, setLoaded] = useState(false)
  const [errored, setErrored] = useState(false)
  const [usingFallback, setUsingFallback] = useState(false)

  // Reset load/error/fallback state whenever the source changes (row recycling).
  useEffect(() => {
    setLoaded(false)
    setErrored(false)
    setUsingFallback(false)
  }, [src, fallbackSrc])

  const roundedClass = rounded === 'full' ? 'rounded-full' : 'rounded-md'

  const sizeStyle: React.CSSProperties =
    typeof size === 'number' ? { width: size, height: size } : {}
  const sizeClass = size === 'full' ? 'w-full h-full' : ''

  const activeSrc = usingFallback ? fallbackSrc : src

  function handleError() {
    // On the first failure, try the fallback (e.g. album art) before giving up.
    if (!usingFallback && fallbackSrc && fallbackSrc !== src) {
      setUsingFallback(true)
      setLoaded(false)
    } else {
      setErrored(true)
    }
  }

  const showPlaceholder = !src || errored
  const showSkeleton = !!activeSrc && !errored && !loaded

  return (
    <div
      className={[
        'relative overflow-hidden flex-none bg-raised',
        roundedClass,
        sizeClass,
        className,
      ]
        .filter(Boolean)
        .join(' ')}
      style={sizeStyle}
    >
      {showPlaceholder ? (
        <div
          data-testid="cover-placeholder"
          className="absolute inset-0 flex items-center justify-center text-text-muted"
        >
          <Icon name="music" className="w-2/5 h-2/5 opacity-40" />
        </div>
      ) : (
        <>
          {showSkeleton && (
            <Skeleton
              data-testid="cover-skeleton"
              className="absolute inset-0 w-full h-full"
              rounded={rounded}
            />
          )}
          <img
            src={activeSrc}
            alt={alt}
            loading="lazy"
            onLoad={() => setLoaded(true)}
            onError={handleError}
            className={[
              'w-full h-full object-cover',
              roundedClass,
              loaded ? 'opacity-100' : 'opacity-0',
            ].join(' ')}
          />
        </>
      )}
    </div>
  )
}
