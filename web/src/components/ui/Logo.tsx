interface LogoProps {
  /** Text-size utility for the wordmark, e.g. "text-2xl". */
  textClassName?: string
}

/**
 * Brand mark: the "Reverb." wordmark with the accent-coloured period. There is
 * no separate icon — the wordmark is the logo everywhere (the favicon and PWA
 * icons use its "R." monogram).
 */
export function Logo({ textClassName = 'text-2xl' }: LogoProps) {
  return (
    <span className={`inline-flex select-none font-bold tracking-tight text-text-primary ${textClassName}`}>
      Reverb<span className="text-accent">.</span>
    </span>
  )
}
