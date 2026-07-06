import { render, screen } from '@testing-library/react'
import { StatusDonutChart } from '../StatusDonutChart'

describe('StatusDonutChart', () => {
  it('renders the donut chart with correct data', () => {
    render(<StatusDonutChart counts={{ ready: 3, degraded: 0, notReady: 1, unknown: 0 }} subtitle="total" />)
    expect(screen.getByTestId('chart-donut')).toBeInTheDocument()
    expect(screen.getByTestId('chart-donut')).toHaveAttribute('data-title', '4')
    expect(screen.getByTestId('chart-donut')).toHaveAttribute('data-subtitle', 'total')
  })

  it('renders segment data for each status category', () => {
    render(<StatusDonutChart counts={{ ready: 5, degraded: 1, notReady: 2, unknown: 1 }} subtitle="total" />)
    expect(screen.getByTestId('donut-segment-Ready')).toHaveTextContent('Ready: 5')
    expect(screen.getByTestId('donut-segment-Degraded')).toHaveTextContent('Degraded: 1')
    expect(screen.getByTestId('donut-segment-Not Ready')).toHaveTextContent('Not Ready: 2')
    expect(screen.getByTestId('donut-segment-Unknown')).toHaveTextContent('Unknown: 1')
  })

  it('shows zero total when all counts are zero', () => {
    render(<StatusDonutChart counts={{ ready: 0, degraded: 0, notReady: 0, unknown: 0 }} subtitle="total" />)
    expect(screen.getByTestId('chart-donut')).toHaveAttribute('data-title', '0')
  })
})
