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
import { cpTypeSegment } from '../utils/cpTypeSegment'
import { clusterDetailLink } from '../utils/linkUtils'
import { fuzzyCaseInsensitive } from '../utils/filterUtils'
import type { RowSearchFilter } from '../utils/filterUtils'
import { useMeshTranslation } from '../utils/i18nUtils'

const compareCpMeshID = (a: EnrichedControlPlane, b: EnrichedControlPlane) =>
  (a.meshID ?? '').localeCompare(b.meshID ?? '')
const compareCpStatusRank = (a: EnrichedControlPlane, b: EnrichedControlPlane) =>
  getStatusRank(a.status?.conditions) - getStatusRank(b.status?.conditions)
const cpTypeRank = (cp: EnrichedControlPlane) => cp.managedBy ? 0 : cp.meshID ? 1 : 2
const compareCpType = (a: EnrichedControlPlane, b: EnrichedControlPlane) =>
  cpTypeRank(a) - cpTypeRank(b)

function buildColumns(t: (key: string) => string): TableColumn<EnrichedControlPlane>[] {
  return [
    {
      title: t('Mesh ID'),
      id: 'meshID',
      sort: (data: EnrichedControlPlane[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * compareCpMeshID(a, b))
      },
    },
    {
      title: t('Type'),
      id: 'type',
      sort: (data: EnrichedControlPlane[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * compareCpType(a, b))
      },
    },
    { title: t('Name'), id: 'name', sort: 'metadata.name' },
    { title: t('Cluster'), id: 'cluster', sort: 'clusterName' },
    { title: t('Namespace'), id: 'namespace', sort: 'controlPlaneNamespace' },
    { title: t('Version'), id: 'version', sort: 'version' },
    { title: t('Created'), id: 'created', sort: 'metadata.creationTimestamp' },
    {
      title: t('Status'),
      id: 'status',
      sort: (data: EnrichedControlPlane[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * compareCpStatusRank(a, b))
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
      <TableData id="meshID" activeColumnIDs={activeColumnIDs}>
        {obj.managedBy ? (
          <Link to={`/fleet-mesh/meshes/managed/${encodeURIComponent(obj.managedBy.namespace)}/${encodeURIComponent(obj.managedBy.name)}`}>
            {obj.meshID ?? '-'}
          </Link>
        ) : obj.meshID ? (
          <Link to={`/fleet-mesh/meshes/discovered/${encodeURIComponent(obj.meshID)}`}>
            {obj.meshID}
          </Link>
        ) : (
          <span>-</span>
        )}
      </TableData>
      <TableData id="type" activeColumnIDs={activeColumnIDs}>
        {obj.managedBy ? t('Managed') : obj.meshID ? t('Discovered') : t('Standalone')}
      </TableData>
      <TableData id="name" activeColumnIDs={activeColumnIDs}>
        <Link to={`/fleet-mesh/control-planes/${cpTypeSegment(obj)}/${encodeURIComponent(obj.clusterName)}/${encodeURIComponent(obj.metadata.name)}`}>
          {obj.metadata.name}
        </Link>
      </TableData>
      <TableData id="cluster" activeColumnIDs={activeColumnIDs}>
        <Link to={clusterDetailLink(obj.clusterName)}>
          {obj.clusterName}
        </Link>
      </TableData>
      <TableData id="namespace" activeColumnIDs={activeColumnIDs}>
        {obj.controlPlaneNamespace ?? '-'}
      </TableData>
      <TableData id="version" activeColumnIDs={activeColumnIDs}>
        {obj.version ?? '-'}
      </TableData>
      <TableData id="created" activeColumnIDs={activeColumnIDs}>
        {obj.metadata.creationTimestamp ? <Timestamp timestamp={obj.metadata.creationTimestamp} /> : '-'}
      </TableData>
      <TableData id="status" activeColumnIDs={activeColumnIDs}>
        {obj.status?.conditions ? (
          <MeshStatus conditions={obj.status.conditions} conditionType="Ready" isCompact />
        ) : (
          <Label color="grey">{t('Unknown')}</Label>
        )}
      </TableData>
    </>
  )
}

function buildSearchFilters(t: (key: string) => string): RowSearchFilter<EnrichedControlPlane>[] {
  return [
    {
      filter: (input, obj) => fuzzyCaseInsensitive(input.selected?.[0], obj.meshID ?? ''),
      filterGroupName: t('Mesh ID'),
      placeholder: t('Filter by mesh ID...'),
      type: 'meshID',
    },
    {
      filter: (input, obj) => {
        const typeLabel = obj.managedBy ? 'managed' : obj.meshID ? 'discovered' : 'standalone'
        return fuzzyCaseInsensitive(input.selected?.[0], typeLabel)
      },
      filterGroupName: t('Type'),
      placeholder: t('Filter by type...'),
      type: 'type',
    },
    {
      filter: (input, obj) => fuzzyCaseInsensitive(input.selected?.[0], obj.clusterName),
      filterGroupName: t('Cluster'),
      placeholder: t('Filter by cluster...'),
      type: 'cluster',
    },
    {
      filter: (input, obj) => fuzzyCaseInsensitive(input.selected?.[0], obj.controlPlaneNamespace ?? ''),
      filterGroupName: t('Namespace'),
      placeholder: t('Filter by namespace...'),
      type: 'namespace',
    },
    {
      filter: (input, obj) => fuzzyCaseInsensitive(input.selected?.[0], obj.version ?? ''),
      filterGroupName: t('Version'),
      placeholder: t('Filter by version...'),
      type: 'version',
    },
  ]
}

const ControlPlanesPage: FC = () => {
  const { t } = useMeshTranslation()
  const { results: searchResults, loaded: searchLoaded, error: searchError, isFleetAvailable } = useDiscoveredControlPlanes()
  const [mcms] = useMultiClusterMeshes()
  const [enrichedPlanes, , , enrichmentError] = useEnrichedControlPlanes(searchResults, mcms ?? [])

  const columns = useMemo(() => buildColumns(t), []) // eslint-disable-line react-hooks/exhaustive-deps
  const searchFilters = useMemo(() => buildSearchFilters(t), []) // eslint-disable-line react-hooks/exhaustive-deps
  const [staticData, filteredData, onFilterChange] = useListPageFilter(enrichedPlanes, searchFilters as any)
  const [activeColumns, userSettingsLoaded] = useActiveColumns({
    columns,
    showNamespaceOverride: false,
    columnManagementID: 'fleet-service-mesh~control-planes',
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
          rowSearchFilters={searchFilters as any}
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

/** Fleet-wide list page showing all discovered Istio control planes across managed clusters. */
export default ControlPlanesPage
