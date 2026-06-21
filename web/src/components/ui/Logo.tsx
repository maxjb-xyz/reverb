import { useState } from 'react'

interface LogoProps {
  /** Size utilities for the image, e.g. "h-8 w-auto". */
  className?: string
  /** Text-size utility for the fallback wordmark, e.g. "text-2xl". */
  fallbackClassName?: string
}

/**
 * Brand logo. Renders `/logo.svg` (drop your file at `web/public/logo.svg`). If
 * that file is missing or fails to load it falls back to the text wordmark, so
 * the UI never shows a broken image before the asset is added.
 */
export function Logo({ className = 'h-8 w-auto', fallbackClassName = 'text-2xl' }: LogoProps) {
  const [failed, setFailed] = useState(false)

  if (failed) {
    return (
      <span className={`select-none font-bold tracking-tight text-text-primary ${fallbackClassName}`}>
        Reverb<span className="text-accent">.</span>
      </span>
    )
  }

  return (
    <img
      src="/logo.svg"
      alt="Reverb"
      className={`select-none ${className}`}
      onError={() => setFailed(true)}
    />
  )
}
