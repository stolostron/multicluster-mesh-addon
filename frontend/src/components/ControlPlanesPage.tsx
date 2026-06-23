import { useMemo } from 'react'
import type { FC } from 'react'
import { Link } from 'react-router-dom-v5-compat'
import {
  ListPageHeader,
  ListPageBody,
  ListPageFilter,
  VirtualizedTable,
  TableData,
  useListPageFilter,
  useActiveColumns,
  Timestamp,
} from '@openshift-console/dynamic-plugin-sdk'
import type { TableColumn, RowProps } from '@openshift-console/dynamic-plugin-sdk'
import {
  EmptyState,
  EmptyStateBody,
  Label,
  PageSection,
  Title,
} from '@patternfly/react-core'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from '../hooks/useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from '../hooks/useEnrichedControlPlanes'
import type { EnrichedControlPlane } from '../types/istio'
import { MeshStatus, getStatusRank } from './MeshStatus'
import { useMeshTranslation } from '../utils/i18nUtils'

function buildColumns(t: (key: string) => string): TableColumn<EnrichedControlPlane>[] {
  return [
    { title: t('Cluster'), id: 'cluster', sort: 'clusterName' },
    { title: t('Name'), id: 'name', sort: 'metadata.name' },
    { title: t('Namespace'), id: 'namespace', sort: 'controlPlaneNamespace' },
    { title: t('Version'), id: 'version', sort: 'version' },
    { title: t('Mesh ID'), id: 'meshID', sort: 'meshID' },
    { title: t('Network'), id: 'network', sort: 'network' },
    {
      title: t('Managed By'),
      id: 'managedBy',
      sort: (data: EnrichedControlPlane[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) =>
          dir * ((a.managedBy?.name ?? '').localeCompare(b.managedBy?.name ?? '')),
        )
      },
    },
    { title: t('Age'), id: 'age', sort: 'metadata.creationTimestamp' },
    {
      title: t('Status'),
      id: 'status',
      sort: (data: EnrichedControlPlane[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort(
          (a, b) => dir * (getStatusRank(a.status?.conditions) - getStatusRank(b.status?.conditions)),
        )
      },
    },
  ]
}

const NoControlPlanesMsg: FC = () => {
  const { t } = useMeshTranslation()
  return (
    <EmptyState variant="xs">
      <EmptyStateBody>{t('No control planes discovered across the fleet.')}</EmptyStateBody>
    </EmptyState>
  )
}

const NoMatchMsg: FC = () => {
  const { t } = useMeshTranslation()
  return (
    <EmptyState variant="xs">
      <EmptyStateBody>{t('No control planes match the current filter.')}</EmptyStateBody>
    </EmptyState>
  )
}

const ControlPlaneRow: FC<RowProps<EnrichedControlPlane>> = ({ obj, activeColumnIDs }) => {
  const { t } = useMeshTranslation()
  return (
    <>
      <TableData id="cluster" activeColumnIDs={activeColumnIDs}>
        <Link to={`/multicloud/infrastructure/clusters/details/${obj.clusterName}/${obj.clusterName}/overview`}>
          {obj.clusterName}
        </Link>
      </TableData>
      <TableData id="name" activeColumnIDs={activeColumnIDs}>
        <Link to={`/control-planes/${encodeURIComponent(obj.clusterName)}/${encodeURIComponent(obj.metadata.name)}`}>
          {obj.metadata.name}
        </Link>
      </TableData>
      <TableData id="namespace" activeColumnIDs={activeColumnIDs}>
        {obj.controlPlaneNamespace ?? '-'}
      </TableData>
      <TableData id="version" activeColumnIDs={activeColumnIDs}>
        {obj.version ?? '-'}
      </TableData>
      <TableData id="meshID" activeColumnIDs={activeColumnIDs}>
        {obj.meshID ?? '-'}
      </TableData>
      <TableData id="network" activeColumnIDs={activeColumnIDs}>
        {obj.network ?? '-'}
      </TableData>
      <TableData id="managedBy" activeColumnIDs={activeColumnIDs}>
        {obj.managedBy ? (
          <Link to={`/service-mesh/${obj.managedBy.namespace}/${obj.managedBy.name}`}>
            <Label color="blue" isCompact>{obj.managedBy.name}</Label>
          </Link>
        ) : '-'}
      </TableData>
      <TableData id="age" activeColumnIDs={activeColumnIDs}>
        {obj.metadata.creationTimestamp ? <Timestamp timestamp={obj.metadata.creationTimestamp} /> : '-'}
      </TableData>
      <TableData id="status" activeColumnIDs={activeColumnIDs}>
        {obj.status?.conditions ? (
          <MeshStatus conditions={obj.status.conditions} conditionType="Ready" />
        ) : (
          <Label color="grey">{t('Unknown')}</Label>
        )}
      </TableData>
    </>
  )
}

const ControlPlanesPage: FC = () => {
  const { t } = useMeshTranslation()
  const { results: searchResults, loaded: searchLoaded, error: searchError, isFleetAvailable } = useDiscoveredControlPlanes()
  const [mcms] = useMultiClusterMeshes()
  const [enrichedPlanes, , , enrichmentError] = useEnrichedControlPlanes(searchResults, mcms ?? [])

  const columns = useMemo(() => buildColumns(t), [t])
  const [staticData, filteredData, onFilterChange] = useListPageFilter(enrichedPlanes)
  const [activeColumns, userSettingsLoaded] = useActiveColumns({
    columns,
    showNamespaceOverride: false,
    columnManagementID: 'sailoperator.io~v1~Istio',
  })

  if (searchLoaded && !searchError && searchResults.length === 0 && !isFleetAvailable) {
    return (
      <>
        <ListPageHeader title={t('Control Planes')} />
        <PageSection>
          <EmptyState>
            <Title headingLevel="h2" size="lg">{t('This page requires Red Hat Advanced Cluster Management.')}</Title>
          </EmptyState>
        </PageSection>
      </>
    )
  }

  return (
    <>
      <ListPageHeader title={t('Control Planes')} />
      <ListPageBody>
        <ListPageFilter
          data={staticData}
          loaded={searchLoaded}
          onFilterChange={onFilterChange}
          hideLabelFilter
        />
        {userSettingsLoaded && (
          <VirtualizedTable<EnrichedControlPlane>
            data={filteredData}
            unfilteredData={enrichedPlanes}
            loaded={searchLoaded}
            loadError={searchError ?? enrichmentError}
            columns={activeColumns}
            Row={ControlPlaneRow}
            NoDataEmptyMsg={NoControlPlanesMsg}
            EmptyMsg={NoMatchMsg}
          />
        )}
      </ListPageBody>
    </>
  )
}

export default ControlPlanesPage
