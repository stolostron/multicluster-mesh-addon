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

const columns: TableColumn<MultiClusterMesh>[] = [
  {
    title: 'Name',
    id: 'name',
    sort: 'metadata.name',
  },
  {
    title: 'Namespace',
    id: 'namespace',
    sort: 'metadata.namespace',
  },
  {
    title: 'Cluster Set',
    id: 'clusterSet',
    sort: 'spec.clusterSet',
  },
  {
    title: 'Clusters',
    id: 'clusters',
    sort: (data, sortDirection) => {
      const dir = sortDirection === 'asc' ? 1 : -1
      return [...data].sort((a, b) =>
        dir * ((a.status?.clusterStatus?.length ?? 0) - (b.status?.clusterStatus?.length ?? 0))
      )
    },
  },
  {
    title: 'Trust',
    id: 'trust',
    sort: 'spec.security.trust.certManager.issuerRef.name',
  },
  {
    title: 'Age',
    id: 'age',
    sort: 'metadata.creationTimestamp',
  },
  {
    title: 'Status',
    id: 'status',
    sort: (data, sortDirection) => {
      const dir = sortDirection === 'asc' ? 1 : -1
      return [...data].sort((a, b) =>
        dir * (getStatusRank(a.status?.conditions) - getStatusRank(b.status?.conditions))
      )
    },
  },
]

const NoMeshesMsg: React.FC = () => (
  <EmptyState variant="xs">
    <EmptyStateBody>No meshes have been created yet.</EmptyStateBody>
  </EmptyState>
)

const NoMatchMsg: React.FC = () => (
  <EmptyState variant="xs">
    <EmptyStateBody>No meshes match the current filter.</EmptyStateBody>
  </EmptyState>
)

const MeshRow: React.FC<RowProps<MultiClusterMesh>> = ({ obj, activeColumnIDs }) => {
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
        {obj.spec.clusterSet}
      </TableData>
      <TableData id="clusters" activeColumnIDs={activeColumnIDs}>
        {obj.status?.clusterStatus?.length ?? 0}
      </TableData>
      <TableData id="trust" activeColumnIDs={activeColumnIDs}>
        {issuerName
          ? <Label color="green" isCompact>Configured</Label>
          : <Label color="grey" isCompact>Not configured</Label>}
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
  const [staticData, filteredData, onFilterChange] = useListPageFilter(meshes)
  const [activeColumns, userSettingsLoaded] = useActiveColumns({
    columns,
    showNamespaceOverride: false,
    columnManagementID: 'mesh.open-cluster-management.io~v1alpha1~MultiClusterMesh',
  })

  return (
    <>
      <ListPageHeader title="Meshes" />
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
