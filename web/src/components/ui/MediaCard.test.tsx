import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MediaCard } from './MediaCard'

describe('MediaCard', () => {
  it('renders the title', () => {
    render(<MediaCard title="OK Computer" />)
    expect(screen.getByText('OK Computer')).toBeInTheDocument()
  })

  it('renders the subtitle when provided', () => {
    render(<MediaCard title="OK Computer" subtitle="Radiohead · 1997" />)
    expect(screen.getByText('Radiohead · 1997')).toBeInTheDocument()
  })

  it('does not render subtitle element when omitted', () => {
    const { container } = render(<MediaCard title="OK Computer" />)
    expect(container.querySelector('[data-testid="mediacard-subtitle"]')).not.toBeInTheDocument()
  })

  it('renders a Cover (img or placeholder) when coverId is provided', () => {
    const { container } = render(<MediaCard title="OK Computer" coverId="art-123" />)
    // Cover renders either an img or a placeholder — either way the wrapper exists
    const cover = container.querySelector('[data-testid="mediacard-cover"]')
    expect(cover).toBeInTheDocument()
  })

  it('fires onPlay when the play button is clicked (not onClick)', () => {
    const onPlay = vi.fn()
    const onClick = vi.fn()
    render(<MediaCard title="OK Computer" onPlay={onPlay} onClick={onClick} />)
    const playBtn = screen.getByRole('button', { name: /play/i })
    fireEvent.click(playBtn)
    expect(onPlay).toHaveBeenCalledTimes(1)
    expect(onClick).not.toHaveBeenCalled()
  })

  it('fires onClick when the card body is clicked', () => {
    const onClick = vi.fn()
    render(<MediaCard title="OK Computer" onClick={onClick} />)
    const card = screen.getByRole('button', { name: /OK Computer/i })
    fireEvent.click(card)
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('renders the badge slot when provided', () => {
    render(<MediaCard title="OK Computer" badge={<span>Downloaded</span>} />)
    expect(screen.getByText('Downloaded')).toBeInTheDocument()
  })

  it('applies rounded-full class when rounded="full"', () => {
    const { container } = render(<MediaCard title="Artist" rounded="full" />)
    // The Cover inside should have rounded-full
    const cover = container.querySelector('[data-testid="mediacard-cover"]')
    expect(cover?.className).toMatch(/rounded-full/)
  })

  it('has bg-raised on the card root', () => {
    const { container } = render(<MediaCard title="Test" />)
    const root = container.firstChild as HTMLElement
    expect(root.className).toMatch(/bg-raised/)
  })

  it('play button has focus-visible ring', () => {
    render(<MediaCard title="OK Computer" onPlay={vi.fn()} />)
    const playBtn = screen.getByRole('button', { name: /play/i })
    expect(playBtn.className).toMatch(/focus-visible:ring/)
  })

  it('renders coverage chip when coverage prop is provided (partial)', () => {
    render(<MediaCard title="OK Computer" coverage={{ state: 'partial', owned: 7, total: 10 }} />)
    expect(screen.getByText('7/10')).toBeInTheDocument()
  })

  it('coverage chip takes precedence over badge when both are provided', () => {
    render(
      <MediaCard
        title="OK Computer"
        coverage={{ state: 'partial', owned: 7, total: 10 }}
        badge={<span>BadgeText</span>}
      />
    )
    expect(screen.getByText('7/10')).toBeInTheDocument()
    expect(screen.queryByText('BadgeText')).not.toBeInTheDocument()
  })

  it('renders download button when onDownload provided and no onPlay', () => {
    const onDownload = vi.fn()
    render(<MediaCard title="OK Computer" onDownload={onDownload} />)
    const dlBtn = screen.getByRole('button', { name: 'Download OK Computer' })
    expect(dlBtn).toBeInTheDocument()
  })

  it('fires onDownload and not onClick when download button clicked', () => {
    const onDownload = vi.fn()
    const onClick = vi.fn()
    render(<MediaCard title="OK Computer" onDownload={onDownload} onClick={onClick} />)
    const dlBtn = screen.getByRole('button', { name: 'Download OK Computer' })
    fireEvent.click(dlBtn)
    expect(onDownload).toHaveBeenCalledTimes(1)
    expect(onClick).not.toHaveBeenCalled()
  })

  it('does not render download button when onPlay is also provided', () => {
    render(<MediaCard title="OK Computer" onPlay={vi.fn()} onDownload={vi.fn()} />)
    expect(screen.queryByRole('button', { name: /download/i })).not.toBeInTheDocument()
  })

  it('renders progress ring and hides download button when downloadProgress.active=true (determinate)', () => {
    render(
      <MediaCard
        title="OK Computer"
        onDownload={vi.fn()}
        downloadProgress={{ active: true, value: 40, indeterminate: false }}
      />,
    )
    // ProgressRing renders an SVG with aria-label "40% complete"
    expect(screen.getByRole('img', { name: /40%/i })).toBeInTheDocument()
    // Plain download button must NOT be present while ring is shown
    expect(screen.queryByRole('button', { name: /download/i })).not.toBeInTheDocument()
  })

  it('renders indeterminate progress ring when downloadProgress.indeterminate=true', () => {
    render(
      <MediaCard
        title="OK Computer"
        onDownload={vi.fn()}
        downloadProgress={{ active: true, value: 0, indeterminate: true }}
      />,
    )
    // Indeterminate ProgressRing has aria-label "Loading"
    expect(screen.getByRole('img', { name: /loading/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /download/i })).not.toBeInTheDocument()
  })

  it('still renders download button when downloadProgress.active=false', () => {
    render(
      <MediaCard
        title="OK Computer"
        onDownload={vi.fn()}
        downloadProgress={{ active: false, value: 0, indeterminate: false }}
      />,
    )
    expect(screen.getByRole('button', { name: 'Download OK Computer' })).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: /loading|% complete/i })).not.toBeInTheDocument()
  })

  it('uses coverSrc directly as the img src when provided', () => {
    const { container } = render(
      <MediaCard title="Kid A" coverSrc="https://cdn.example.com/kida.jpg" />,
    )
    const img = container.querySelector('img')
    expect(img).toBeInTheDocument()
    expect(img?.src).toBe('https://cdn.example.com/kida.jpg')
  })

  it('coverSrc takes precedence over coverId', () => {
    const { container } = render(
      <MediaCard title="Kid A" coverSrc="https://cdn.example.com/kida.jpg" coverId="art-123" />,
    )
    const img = container.querySelector('img')
    expect(img?.src).toBe('https://cdn.example.com/kida.jpg')
  })

  // ── Ghost card tests ────────────────────────────────────────────────────────

  it('ghost card has border-dashed on the root button', () => {
    const { container } = render(<MediaCard title="Ghost Album" ghost={true} />)
    const root = container.firstChild as HTMLElement
    expect(root.className).toMatch(/border-dashed/)
  })

  it('ghost card suppresses the play button even when onPlay is provided', () => {
    render(<MediaCard title="Ghost Album" onPlay={vi.fn()} ghost={true} />)
    expect(screen.queryByLabelText('Play Ghost Album')).not.toBeInTheDocument()
  })

  it('ghost card still renders the download button when onDownload is provided', () => {
    const onDownload = vi.fn()
    render(<MediaCard title="Ghost Album" onDownload={onDownload} ghost={true} />)
    const dlBtn = screen.getByRole('button', { name: 'Download Ghost Album' })
    expect(dlBtn).toBeInTheDocument()
  })

  it('ghost card does not suppress download button even when onPlay is also provided', () => {
    const onDownload = vi.fn()
    render(
      <MediaCard
        title="Ghost Album"
        onPlay={vi.fn()}
        onDownload={onDownload}
        ghost={true}
      />,
    )
    const dlBtn = screen.getByRole('button', { name: 'Download Ghost Album' })
    expect(dlBtn).toBeInTheDocument()
  })

  it('non-ghost card does not have border-dashed', () => {
    const { container } = render(<MediaCard title="Regular Album" ghost={false} />)
    const root = container.firstChild as HTMLElement
    expect(root.className).not.toMatch(/border-dashed/)
  })
})
