import type { FC } from 'react'

export const ChartDonut: FC<{
  data?: { x: string; y: number }[]
  title?: string
  subTitle?: string
  [key: string]: unknown
}> = ({ data, title, subTitle }) => (
  <div data-testid="chart-donut" data-title={title} data-subtitle={subTitle}>
    {data?.map((d) => (
      <span key={d.x} data-testid={`donut-segment-${d.x}`}>{`${d.x}: ${d.y}`}</span>
    ))}
  </div>
)
