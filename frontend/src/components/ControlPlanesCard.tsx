import { useMemo } from 'react'
import type { FC } from 'react'
import { Link } from 'react-router-dom-v5-compat'
import {
  Card,
  CardBody,
  CardTitle,
  Label,
} from '@patternfly/react-core'
import type { EnrichedControlPlane } from '../types/istio'
import { categorizeCp } from '../types/istio'
import { MeshStatus } from './MeshStatus'
import { VirtualFilterTable } from './VirtualFilterTable'
import type { CategoryLabel, VirtualFilterColumn } from './VirtualFilterTable'
import { cpTypeSegment } from '../utils/cpTypeSegment'
import { clusterDetailLink } from '../utils/linkUtils'
import { useMeshTranslation } from '../utils/i18nUtils'

const cpRowKey = (cp: EnrichedControlPlane) => `${cp.clusterName}/${cp.metadata.name}`
const cpSearchMatch = (cp: EnrichedControlPlane, query: string) => {
  const q = query.toLowerCase()
  return cp.clusterName.toLowerCase().includes(q) || cp.metadata.name.toLowerCase().includes(q)
}

const CATEGORY_LABELS: CategoryLabel[] = [
  { key: 'all', label: 'All ({{count}})' },
  { key: 'ready', label: 'Ready ({{count}})' },
  { key: 'notReady', label: 'Not Ready ({{count}})' },
  { key: 'unknown', label: 'Unknown ({{count}})' },
]

const ControlPlanesCard: FC<{ planes: EnrichedControlPlane[] }> = ({ planes }) => {
  const { t } = useMeshTranslation()

  const columns = useMemo<VirtualFilterColumn<EnrichedControlPlane>[]>(() => [
    {
      key: 'cluster',
      label: 'Cluster',
      render: (cp) => (
        <Link to={clusterDetailLink(cp.clusterName)}>{cp.clusterName}</Link>
      ),
      width: '25%',
    },
    {
      key: 'name',
      label: 'Name',
      render: (cp) => (
        <Link to={`/fleet-mesh/control-planes/${cpTypeSegment(cp)}/${encodeURIComponent(cp.clusterName)}/${encodeURIComponent(cp.metadata.name)}`}>
          {cp.metadata.name}
        </Link>
      ),
      width: '20%',
    },
    { key: 'namespace', label: 'Namespace', render: (cp) => cp.controlPlaneNamespace ?? '-', width: '20%' },
    { key: 'version', label: 'Version', render: (cp) => cp.version ?? '-', width: '15%' },
    {
      key: 'status',
      label: 'Status',
      render: (cp) => cp.status?.conditions
        ? <MeshStatus conditions={cp.status.conditions} conditionType="Ready" isCompact />
        : <Label color="grey">{t('Unknown')}</Label>,
      width: '20%',
    },
  ], [t])

  if (planes.length === 0) return null

  return (
    <Card isCompact>
      <CardTitle><strong>{t('Control Planes ({{count}})', { count: planes.length })}</strong></CardTitle>
      <CardBody>
        <VirtualFilterTable
          categorize={categorizeCp}
          categoryLabels={CATEGORY_LABELS}
          columns={columns}
          emptyMessage="No control planes match the current filter."
          items={planes}
          rowKey={cpRowKey}
          searchMatch={cpSearchMatch}
          searchPlaceholder="Filter by cluster name"
        />
      </CardBody>
    </Card>
  )
}

export { ControlPlanesCard }
