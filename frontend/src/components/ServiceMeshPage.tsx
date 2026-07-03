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
import { fuzzyCaseInsensitive } from '../utils/filterUtils'
import type { RowSearchFilter } from '../utils/filterUtils'
import { useMeshTranslation } from '../utils/i18nUtils'

function buildColumns(t: (key: string) => string): TableColumn<FleetMeshItem>[] {
  return [
    {
      title: t('Mesh ID'),
      id: 'meshID',
      sort: (data: FleetMeshItem[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * (a.meshID ?? '').localeCompare(b.meshID ?? ''))
      },
    },
    {
      title: t('Type'),
      id: 'type',
      sort: (data: FleetMeshItem[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * a.kind.localeCompare(b.kind))
      },
    },
    {
      title: t('Name'),
      id: 'name',
      sort: (data: FleetMeshItem[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * a.metadata.name.localeCompare(b.metadata.name))
      },
    },
    {
      title: t('Cluster Set'),
      id: 'clusterSet',
      sort: (data: FleetMeshItem[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * (a.clusterSet ?? '').localeCompare(b.clusterSet ?? ''))
      },
    },
    {
      title: t('Clusters'),
      id: 'clusters',
      sort: (data: FleetMeshItem[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * (a.clusterCount - b.clusterCount))
      },
    },
    {
      title: t('Trust'),
      id: 'trust',
      sort: (data: FleetMeshItem[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * (a.trustIssuer ?? '').localeCompare(b.trustIssuer ?? ''))
      },
    },
    {
      title: t('Status'),
      id: 'status',
      sort: (data: FleetMeshItem[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort((a, b) => dir * (a.statusRank - b.statusRank))
      },
    },
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
          <Link to={`/multicloud/infrastructure/clusters/sets/details/${encodeURIComponent(obj.clusterSet)}/overview`}>
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
  const columns = useMemo(() => buildColumns(t), [t])
  const searchFilters = useMemo(() => buildSearchFilters(t), [t])
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
