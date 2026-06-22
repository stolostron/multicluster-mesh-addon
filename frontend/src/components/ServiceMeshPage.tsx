import * as React from 'react'
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
} from '@patternfly/react-core'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import type { MultiClusterMesh } from '../types/multiClusterMesh'
import { MeshStatus, getStatusRank } from './MeshStatus'
import { useMeshTranslation } from '../utils/i18nUtils'

function buildColumns(t: (key: string) => string): TableColumn<MultiClusterMesh>[] {
  return [
    { title: t('Name'), id: 'name', sort: 'metadata.name' },
    { title: t('Namespace'), id: 'namespace', sort: 'metadata.namespace' },
    { title: t('Cluster Set'), id: 'clusterSet', sort: 'spec.clusterSet' },
    {
      title: t('Clusters'),
      id: 'clusters',
      sort: (data: MultiClusterMesh[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort(
          (a, b) => dir * ((a.status?.clusterStatus?.length ?? 0) - (b.status?.clusterStatus?.length ?? 0)),
        )
      },
    },
    { title: t('Trust'), id: 'trust', sort: 'spec.security.trust.certManager.issuerRef.name' },
    { title: t('Age'), id: 'age', sort: 'metadata.creationTimestamp' },
    {
      title: t('Status'),
      id: 'status',
      sort: (data: MultiClusterMesh[], sortDirection: string) => {
        const dir = sortDirection === 'asc' ? 1 : -1
        return [...data].sort(
          (a, b) => dir * (getStatusRank(a.status?.conditions) - getStatusRank(b.status?.conditions)),
        )
      },
    },
  ]
}

const NoMeshesMsg: React.FC = () => {
  const { t } = useMeshTranslation()
  return (
    <EmptyState variant="xs">
      <EmptyStateBody>{t('No meshes have been created yet.')}</EmptyStateBody>
    </EmptyState>
  )
}

const NoMatchMsg: React.FC = () => {
  const { t } = useMeshTranslation()
  return (
    <EmptyState variant="xs">
      <EmptyStateBody>{t('No meshes match the current filter.')}</EmptyStateBody>
    </EmptyState>
  )
}

const MeshRow: React.FC<RowProps<MultiClusterMesh>> = ({ obj, activeColumnIDs }) => {
  const { t } = useMeshTranslation()
  const issuerName = obj.spec.security?.trust?.certManager?.issuerRef?.name
  return (
    <>
      <TableData id="name" activeColumnIDs={activeColumnIDs}>
        <Link to={`/service-mesh/${obj.metadata?.namespace}/${obj.metadata?.name}`}>
          {obj.metadata?.name ?? '-'}
        </Link>
      </TableData>
      <TableData id="namespace" activeColumnIDs={activeColumnIDs}>
        {obj.metadata?.namespace ?? '-'}
      </TableData>
      <TableData id="clusterSet" activeColumnIDs={activeColumnIDs}>
        <Link to={`/multicloud/infrastructure/clusters/sets/details/${obj.spec.clusterSet}/overview`}>
          {obj.spec.clusterSet}
        </Link>
      </TableData>
      <TableData id="clusters" activeColumnIDs={activeColumnIDs}>
        {obj.status?.clusterStatus?.length ?? 0}
      </TableData>
      <TableData id="trust" activeColumnIDs={activeColumnIDs}>
        {issuerName
          ? <Label color="green" isCompact>{t('Configured')}</Label>
          : <Label color="grey" isCompact>{t('Not configured')}</Label>}
      </TableData>
      <TableData id="age" activeColumnIDs={activeColumnIDs}>
        <Timestamp timestamp={obj.metadata?.creationTimestamp} />
      </TableData>
      <TableData id="status" activeColumnIDs={activeColumnIDs}>
        <MeshStatus conditions={obj.status?.conditions} />
      </TableData>
    </>
  )
}

const ServiceMeshPage: React.FC = () => {
  const [meshes, loaded, error] = useMultiClusterMeshes()
  const { t } = useMeshTranslation()
  const columns = React.useMemo(() => buildColumns(t), [t])
  const [staticData, filteredData, onFilterChange] = useListPageFilter(meshes)
  const [activeColumns, userSettingsLoaded] = useActiveColumns({
    columns,
    showNamespaceOverride: false,
    columnManagementID: 'mesh.open-cluster-management.io~v1alpha1~MultiClusterMesh',
  })

  return (
    <>
      <ListPageHeader title={t('Meshes')} />
      <ListPageBody>
        <ListPageFilter
          data={staticData}
          loaded={loaded}
          onFilterChange={onFilterChange}
          hideLabelFilter
        />
        {userSettingsLoaded && (
          <VirtualizedTable<MultiClusterMesh>
            data={filteredData}
            unfilteredData={meshes}
            loaded={loaded}
            loadError={error}
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

export default ServiceMeshPage
