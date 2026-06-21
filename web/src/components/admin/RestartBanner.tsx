import { Icon } from '../ui'

interface RestartBannerProps {
  show: boolean
}

export function RestartBanner({ show }: RestartBannerProps) {
  if (!show) return null

  return (
    <div
      role="alert"
      className="flex items-center gap-3 rounded-lg border border-warning/30 bg-warning/10 px-4 py-3 text-sm font-semibold text-warning"
    >
      <Icon name="warn" className="w-4 h-4 flex-none" aria-label="Warning" />
      <span>Changes saved — restart Reverb to apply.</span>
    </div>
  )
}
