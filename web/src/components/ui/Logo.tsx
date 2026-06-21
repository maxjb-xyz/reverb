import { useState } from 'react'

interface LogoProps {
  /** Height utility for the icon mark, e.g. "h-8 w-auto". */
  iconClassName?: string
  /** Text-size utility for the wordmark, e.g. "text-2xl". */
  textClassName?: string
}

/**
 * Brand lockup: the Reverb mark + the "Reverb." wordmark. Uses the light icon
 * variant (`/Reverb-Light.svg`, white strokes) so it reads on the dark UI. If
 * the icon asset is missing it shows the wordmark alone — never a broken image.
 * The original `/logo.svg` (dark strokes) is kept for the favicon / light chrome.
 */
export function Logo({ iconClassName = 'h-8 w-auto', textClassName = 'text-2xl' }: LogoProps) {
  const [iconOk, setIconOk] = useState(true)

  return (
    <span className="inline-flex select-none items-center gap-2">
      {iconOk && (
        <img
          src="/Reverb-Light.svg"
          alt=""
          aria-hidden="true"
          className={iconClassName}
          onError={() => setIconOk(false)}
        />
      )}
      <span className={`font-bold tracking-tight text-text-primary ${textClassName}`}>
        Reverb<span className="text-accent">.</span>
      </span>
    </span>
  )
}
