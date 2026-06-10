import * as React from 'react'
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
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import type { MultiClusterMesh } from '../types/multiClusterMesh'
import { MeshStatus } from './MeshStatus'

const columns: TableColumn<MultiClusterMesh>[] = [
  {
    title: 'Name',
    id: 'name',
    sort: 'metadata.name',
  },
  {
    title: 'Cluster Set',
    id: 'clusterSet',
    sort: 'spec.clusterSet',
  },
  {
    title: 'Status',
    id: 'status',
  },
  {
    title: 'Clusters',
    id: 'clusters',
  },
  {
    title: 'Age',
    id: 'age',
    sort: 'metadata.creationTimestamp',
  },
]

const MeshRow: React.FC<RowProps<MultiClusterMesh>> = ({ obj, activeColumnIDs }) => (
  <>
    <TableData id="name" activeColumnIDs={activeColumnIDs}>
      {obj.metadata?.name ?? '-'}
    </TableData>
    <TableData id="clusterSet" activeColumnIDs={activeColumnIDs}>
      {obj.spec.clusterSet}
    </TableData>
    <TableData id="status" activeColumnIDs={activeColumnIDs}>
      <MeshStatus conditions={obj.status?.conditions} />
    </TableData>
    <TableData id="clusters" activeColumnIDs={activeColumnIDs}>
      {obj.status?.clusterStatus?.length ?? 0}
    </TableData>
    <TableData id="age" activeColumnIDs={activeColumnIDs}>
      <Timestamp timestamp={obj.metadata?.creationTimestamp} />
    </TableData>
  </>
)

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
          />
        )}
      </ListPageBody>
    </>
  )
}

export default ServiceMeshPage
