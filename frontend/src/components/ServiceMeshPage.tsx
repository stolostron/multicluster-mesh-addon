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
} from '@openshift-console/dynamic-plugin-sdk'
import type { TableColumn, RowProps } from '@openshift-console/dynamic-plugin-sdk'
import {
  Alert,
  EmptyState,
  EmptyStateBody,
  Label,
  Tooltip,
} from '@patternfly/react-core'
import { ExclamationTriangleIcon } from '@patternfly/react-icons'
import { useFleetMeshItems } from '../hooks/useFleetMeshItems'
import type { FleetMeshItem } from '../types/fleetMesh'
import { MeshStatus } from './MeshStatus'
import { clusterSetDetailLink } from '../utils/linkUtils'
import { fuzzyCaseInsensitive } from '../utils/filterUtils'
import type { RowSearchFilter } from '../utils/filterUtils'
import { useMeshTranslation } from '../utils/i18nUtils'
import { sortWithComparator } from '../utils/tableCallbacks'

const compareMeshClusterCount = (a: FleetMeshItem, b: FleetMeshItem) => a.clusterCount - b.clusterCount
const compareMeshClusterSet = (a: FleetMeshItem, b: FleetMeshItem) => (a.clusterSet ?? '').localeCompare(b.clusterSet ?? '')
const compareMeshID = (a: FleetMeshItem, b: FleetMeshItem) => (a.meshID ?? '').localeCompare(b.meshID ?? '')
const compareMeshName = (a: FleetMeshItem, b: FleetMeshItem) => a.metadata.name.localeCompare(b.metadata.name)
const compareMeshStatusRank = (a: FleetMeshItem, b: FleetMeshItem) => a.statusRank - b.statusRank
const compareMeshTrust = (a: FleetMeshItem, b: FleetMeshItem) => (a.trustIssuer ?? '').localeCompare(b.trustIssuer ?? '')
const compareMeshType = (a: FleetMeshItem, b: FleetMeshItem) => a.kind.localeCompare(b.kind)

function buildColumns(t: (key: string) => string): TableColumn<FleetMeshItem>[] {
  return [
    { title: t('Mesh ID'), id: 'meshID', sort: (data: FleetMeshItem[], dir: string) => sortWithComparator(data, dir, compareMeshID) },
    { title: t('Type'), id: 'type', sort: (data: FleetMeshItem[], dir: string) => sortWithComparator(data, dir, compareMeshType) },
    { title: t('Name'), id: 'name', sort: (data: FleetMeshItem[], dir: string) => sortWithComparator(data, dir, compareMeshName) },
    { title: t('Cluster Set'), id: 'clusterSet', sort: (data: FleetMeshItem[], dir: string) => sortWithComparator(data, dir, compareMeshClusterSet) },
    { title: t('Clusters'), id: 'clusters', sort: (data: FleetMeshItem[], dir: string) => sortWithComparator(data, dir, compareMeshClusterCount) },
    { title: t('Trust'), id: 'trust', sort: (data: FleetMeshItem[], dir: string) => sortWithComparator(data, dir, compareMeshTrust) },
    { title: t('Status'), id: 'status', sort: (data: FleetMeshItem[], dir: string) => sortWithComparator(data, dir, compareMeshStatusRank) },
  ]
}

const NoMeshesMsg: FC = () => {
  const { t } = useMeshTranslation()
  return (
    <EmptyState variant="xs">
      <EmptyStateBody>{t('No managed or discovered meshes found.')}</EmptyStateBody>
    </EmptyState>
  )
}

const NoMatchMsg: FC = () => {
  const { t } = useMeshTranslation()
  return (
    <EmptyState variant="xs">
      <EmptyStateBody>{t('No meshes match the current filter.')}</EmptyStateBody>
    </EmptyState>
  )
}

const MeshRow: FC<RowProps<FleetMeshItem>> = ({ obj, activeColumnIDs }) => {
  const { t } = useMeshTranslation()
  const isManaged = obj.kind === 'managed'

  const nameContent = obj.metadata.name

  return (
    <>
      <TableData id="meshID" activeColumnIDs={activeColumnIDs}>
        {obj.meshID ? (
          <Link to={obj.detailLink}>{obj.meshID}</Link>
        ) : '-'}
      </TableData>
      <TableData id="type" activeColumnIDs={activeColumnIDs}>
        {isManaged ? t('Managed') : t('Discovered')}
      </TableData>
      <TableData id="name" activeColumnIDs={activeColumnIDs}>
        {nameContent}
        {obj.meshIDConflict && (
          <Tooltip content={t('Mesh ID Conflict')}>
            <ExclamationTriangleIcon style={{ color: 'var(--pf-v6-global--warning-color--100)', marginLeft: '0.5rem' }} />
          </Tooltip>
        )}
      </TableData>
      <TableData id="clusterSet" activeColumnIDs={activeColumnIDs}>
        {obj.clusterSet ? (
          <Link to={clusterSetDetailLink(obj.clusterSet)}>
            {obj.clusterSet}
          </Link>
        ) : '-'}
      </TableData>
      <TableData id="clusters" activeColumnIDs={activeColumnIDs}>
        {obj.clusterCount}
      </TableData>
      <TableData id="trust" activeColumnIDs={activeColumnIDs}>
        {isManaged
          ? (obj.trustIssuer
              ? <Label color="green" isCompact>{t('Configured')}</Label>
              : <Label color="grey" isCompact>{t('Not configured')}</Label>)
          : '-'}
      </TableData>
      <TableData id="status" activeColumnIDs={activeColumnIDs}>
        {obj.meshIDConflict
          ? <Label color="red">{t('Mesh ID Conflict')}</Label>
          : <MeshStatus conditions={obj.conditions} isCompact />}
      </TableData>
    </>
  )
}

function buildSearchFilters(t: (key: string) => string): RowSearchFilter<FleetMeshItem>[] {
  return [
    {
      filter: (input, obj) => fuzzyCaseInsensitive(input.selected?.[0], obj.meshID ?? ''),
      filterGroupName: t('Mesh ID'),
      placeholder: t('Filter by mesh ID...'),
      type: 'meshID',
    },
    {
      filter: (input, obj) => fuzzyCaseInsensitive(input.selected?.[0], obj.kind),
      filterGroupName: t('Type'),
      placeholder: t('Filter by type...'),
      type: 'type',
    },
  ]
}

const ServiceMeshPage: FC = () => {
  const {
    items,
    loaded,
    enrichmentError,
    isFleetAvailable,
  } = useFleetMeshItems()
  const { t } = useMeshTranslation()
  const columns = useMemo(() => buildColumns(t), []) // eslint-disable-line react-hooks/exhaustive-deps
  const searchFilters = useMemo(() => buildSearchFilters(t), []) // eslint-disable-line react-hooks/exhaustive-deps
  const [staticData, filteredData, onFilterChange] = useListPageFilter(items, searchFilters as any)
  const [activeColumns, userSettingsLoaded] = useActiveColumns({
    columns,
    showNamespaceOverride: false,
    columnManagementID: 'fleet-service-mesh~unified',
  })

  return (
    <>
      <ListPageHeader title={t('Meshes')} />
      <ListPageBody>
        <ListPageFilter
          data={staticData}
          loaded={loaded}
          onFilterChange={onFilterChange}
          rowSearchFilters={searchFilters as any}
          hideLabelFilter
        />
        {!isFleetAvailable && loaded && (
          <Alert
            variant="info"
            isInline
            isPlain
            title={t('Install Red Hat Advanced Cluster Management to discover unmanaged meshes across the fleet.')}
            style={{ marginBottom: '1rem' }}
          />
        )}
        {!!enrichmentError && loaded && (
          <Alert
            variant="warning"
            isInline
            isPlain
            title={t('Unable to load control plane data. Some meshes may not be shown.')}
            style={{ marginBottom: '1rem' }}
          />
        )}
        {userSettingsLoaded && (
          <VirtualizedTable<FleetMeshItem>
            data={filteredData}
            unfilteredData={items}
            loaded={loaded}
            loadError={null}
            columns={activeColumns}
            Row={MeshRow}
            NoDataEmptyMsg={NoMeshesMsg}
            EmptyMsg={NoMatchMsg}
          />
        )}
      </ListPageBody>
    </>
  )
}

/** List page showing all managed and discovered fleet meshes. */
export default ServiceMeshPage
