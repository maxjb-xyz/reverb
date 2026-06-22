import { render } from '@testing-library/react'
import { CoverageChip } from './CoverageChip'

test('full renders a check, none renders nothing', () => {
  const { container, rerender } = render(<CoverageChip state="full" owned={10} total={10} />)
  expect(container.querySelector('[data-testid="coverage-full"]')).toBeTruthy()
  rerender(<CoverageChip state="none" owned={0} total={12} />)
  expect(container.firstChild).toBeNull()
})

test('partial shows owned/total', () => {
  const { getByText } = render(<CoverageChip state="partial" owned={7} total={10} />)
  expect(getByText('7/10')).toBeTruthy()
})
