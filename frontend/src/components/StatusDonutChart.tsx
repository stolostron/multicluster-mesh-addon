import type { FC } from 'react'
import { ChartDonut } from '@patternfly/react-charts/victory'
import { useMeshTranslation } from '../utils/i18nUtils'

export interface StatusCounts {
  degraded: number
  notReady: number
  ready: number
  unknown: number
}

interface StatusDonutChartProps {
  counts: StatusCounts
  subtitle: string
}

const colorScale = [
  'var(--pf-v6-chart-color-green-300, #4cb140)',
  'var(--pf-v6-chart-color-orange-300, #f4c145)',
  'var(--pf-v6-chart-color-red-100, #c9190b)',
  'var(--pf-v6-chart-color-black-300, #d2d2d2)',
]

export const StatusDonutChart: FC<StatusDonutChartProps> = ({ counts, subtitle }) => {
  const { t } = useMeshTranslation()
  const total = counts.ready + counts.degraded + counts.notReady + counts.unknown

  const data = [
    { x: t('Ready'), y: counts.ready },
    { x: t('Degraded'), y: counts.degraded },
    { x: t('Not Ready'), y: counts.notReady },
    { x: t('Unknown'), y: counts.unknown },
  ]

  const legendData = [
    { name: t('{{count}} Ready', { count: counts.ready }) },
    { name: t('{{count}} Degraded', { count: counts.degraded }) },
    { name: t('{{count}} Not Ready', { count: counts.notReady }) },
    { name: t('{{count}} Unknown', { count: counts.unknown }) },
  ]

  return (
    <div style={{ width: '100%' }}>
      <ChartDonut
        colorScale={colorScale}
        constrainToVisibleArea
        data={data}
        height={120}
        legendData={legendData}
        legendOrientation="vertical"
        legendPosition="right"
        padding={{ bottom: 10, left: 10, right: 140, top: 10 }}
        subTitle={subtitle}
        title={String(total)}
        width={350}
      />
    </div>
  )
}
