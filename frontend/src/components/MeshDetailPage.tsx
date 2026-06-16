import * as React from 'react'
import { useParams, Link } from 'react-router-dom-v5-compat'
import {
  useK8sWatchResource,
  Timestamp,
} from '@openshift-console/dynamic-plugin-sdk'
import {
  Breadcrumb,
  BreadcrumbItem,
  Card,
  CardBody,
  CardTitle,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  EmptyState,
  EmptyStateBody,
  Flex,
  FlexItem,
  Grid,
  GridItem,
  Label,
  PageSection,
  SearchInput,
  Spinner,
  Title,
  ToggleGroup,
  ToggleGroupItem,
} from '@patternfly/react-core'
import type { MultiClusterMesh, K8sCondition, ClusterMeshStatus } from '../types/multiClusterMesh'
import { multiClusterMeshGroupVersionKind } from '../types/multiClusterMesh'
import { MeshStatus } from './MeshStatus'
import { TrustStatusCard } from './TrustStatusCard'

function conditionMessage(condition: K8sCondition): string {
  if (condition.message) return condition.message
  if (condition.reason) return condition.reason
  return condition.status === 'True' ? 'True' : 'False'
}

function statusIcon(status: string): React.ReactNode {
  const color = status === 'True' ? 'green' : 'red'
  return <Label color={color}>{status}</Label>
}

type ClusterStatusCategory = 'all' | 'ready' | 'notReady' | 'unknown'

function categorizeCluster(cs: ClusterMeshStatus): ClusterStatusCategory {
  const op = cs.conditions?.find((c) => c.type === 'OperatorInstalled')
  if (!op) return 'unknown'
  if (op.status === 'True') return 'ready'
  return 'notReady'
}

const ClusterStatusSection: React.FC<{ clusterStatuses: ClusterMeshStatus[] }> = ({ clusterStatuses }) => {
  const [filter, setFilter] = React.useState<ClusterStatusCategory>('all')
  const [search, setSearch] = React.useState('')

  const counts = React.useMemo(() => {
    const result = { ready: 0, notReady: 0, unknown: 0 }
    clusterStatuses.forEach((cs) => {
      const cat = categorizeCluster(cs)
      if (cat !== 'all') result[cat]++
    })
    return result
  }, [clusterStatuses])

  const filtered = React.useMemo(() => {
    return clusterStatuses.filter((cs) => {
      if (filter !== 'all' && categorizeCluster(cs) !== filter) return false
      if (search && !cs.clusterName.toLowerCase().includes(search.toLowerCase())) return false
      return true
    })
  }, [clusterStatuses, filter, search])

  if (clusterStatuses.length === 0) {
    return (
      <Card>
        <CardTitle>Cluster Status (0)</CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <EmptyStateBody>No clusters are part of this mesh yet.</EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  return (
    <Card>
      <CardTitle>Cluster Status ({clusterStatuses.length})</CardTitle>
      <CardBody>
        <Flex style={{ marginBottom: '1rem' }} spaceItems={{ default: 'spaceItemsMd' }}>
          <FlexItem>
            <Label color="green" isCompact>{counts.ready} Ready</Label>
          </FlexItem>
          <FlexItem>
            <Label color="red" isCompact>{counts.notReady} Not Ready</Label>
          </FlexItem>
          <FlexItem>
            <Label color="grey" isCompact>{counts.unknown} Unknown</Label>
          </FlexItem>
        </Flex>

        <Grid hasGutter>
          <GridItem span={12}>
            <Flex style={{ marginBottom: '1rem' }}>
              <FlexItem>
                <ToggleGroup>
                  <ToggleGroupItem
                    text={`All (${clusterStatuses.length})`}
                    isSelected={filter === 'all'}
                    onChange={() => setFilter('all')}
                  />
                  <ToggleGroupItem
                    text={`Ready (${counts.ready})`}
                    isSelected={filter === 'ready'}
                    onChange={() => setFilter('ready')}
                  />
                  <ToggleGroupItem
                    text={`Not Ready (${counts.notReady})`}
                    isSelected={filter === 'notReady'}
                    onChange={() => setFilter('notReady')}
                  />
                  <ToggleGroupItem
                    text={`Unknown (${counts.unknown})`}
                    isSelected={filter === 'unknown'}
                    onChange={() => setFilter('unknown')}
                  />
                </ToggleGroup>
              </FlexItem>
              <FlexItem grow={{ default: 'grow' }}>
                <SearchInput
                  placeholder="Filter by cluster name"
                  value={search}
                  onChange={(_event, value) => setSearch(value)}
                  onClear={() => setSearch('')}
                />
              </FlexItem>
            </Flex>

            <div style={{ maxHeight: '400px', overflowY: 'auto' }}>
              <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid">
                <thead className="pf-v6-c-table__thead">
                  <tr className="pf-v6-c-table__tr">
                    <th className="pf-v6-c-table__th">Cluster</th>
                    <th className="pf-v6-c-table__th">Operator Status</th>
                    <th className="pf-v6-c-table__th">Message</th>
                  </tr>
                </thead>
                <tbody className="pf-v6-c-table__tbody">
                  {filtered.length === 0 ? (
                    <tr className="pf-v6-c-table__tr">
                      <td className="pf-v6-c-table__td" colSpan={3} style={{ textAlign: 'center' }}>
                        No clusters match the current filter.
                      </td>
                    </tr>
                  ) : (
                    filtered.map((cs) => {
                      const operatorCondition = cs.conditions?.find((c) => c.type === 'OperatorInstalled')
                      return (
                        <tr className="pf-v6-c-table__tr" key={cs.clusterName}>
                          <td className="pf-v6-c-table__td">{cs.clusterName}</td>
                          <td className="pf-v6-c-table__td">
                            <MeshStatus conditions={cs.conditions} conditionType="OperatorInstalled" />
                          </td>
                          <td className="pf-v6-c-table__td">{operatorCondition ? conditionMessage(operatorCondition) : '-'}</td>
                        </tr>
                      )
                    })
                  )}
                </tbody>
              </table>
            </div>
          </GridItem>
        </Grid>
      </CardBody>
    </Card>
  )
}

const MeshDetailContent: React.FC<{ ns: string; name: string }> = ({ ns, name }) => {
  const [mesh, loaded, loadError] = useK8sWatchResource<MultiClusterMesh>({
    groupVersionKind: multiClusterMeshGroupVersionKind,
    name,
    namespace: ns,
  })

  if (loadError) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">Error loading mesh</Title>
          <EmptyStateBody>
            {loadError instanceof Error ? loadError.message : String(loadError)}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  if (!loaded) {
    return (
      <PageSection>
        <Spinner aria-label="Loading mesh details" />
      </PageSection>
    )
  }

  if (!mesh) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">Mesh not found</Title>
          <EmptyStateBody>
            MultiClusterMesh &quot;{name}&quot; was not found in namespace &quot;{ns}&quot;.
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  const spec = mesh.spec
  const status = mesh.status
  const clusterStatuses = status?.clusterStatus ?? []
  const conditions = status?.conditions ?? []
  const issuerName = spec.security?.trust?.certManager?.issuerRef?.name

  return (
    <>
      <PageSection variant="light">
        <Breadcrumb>
          <BreadcrumbItem>
            <Link to="/service-mesh">Meshes</Link>
          </BreadcrumbItem>
          <BreadcrumbItem isActive>{mesh.metadata?.name}</BreadcrumbItem>
        </Breadcrumb>
        <Flex alignItems={{ default: 'alignItemsCenter' }} style={{ marginTop: '1rem' }}>
          <FlexItem>
            <Title headingLevel="h1">{mesh.metadata?.name}</Title>
          </FlexItem>
          <FlexItem>
            <MeshStatus conditions={conditions} conditionType="Ready" />
          </FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        <Grid hasGutter>
          <GridItem span={6}>
            <Card isCompact>
              <CardTitle>Overview</CardTitle>
              <CardBody>
                <DescriptionList isHorizontal isCompact>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Cluster Set</DescriptionListTerm>
                    <DescriptionListDescription>{spec.clusterSet}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Control Plane Namespace</DescriptionListTerm>
                    <DescriptionListDescription>{spec.controlPlane?.namespace || 'istio-system'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>cert-manager Issuer</DescriptionListTerm>
                    <DescriptionListDescription>{issuerName || 'Not configured'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Created</DescriptionListTerm>
                    <DescriptionListDescription>
                      <Timestamp timestamp={mesh.metadata?.creationTimestamp} />
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={6}>
            <Card isCompact>
              <CardTitle>OSSM Operator</CardTitle>
              <CardBody>
                <DescriptionList isHorizontal isCompact>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Namespace</DescriptionListTerm>
                    <DescriptionListDescription>{spec.operator?.namespace || '(platform default)'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Channel</DescriptionListTerm>
                    <DescriptionListDescription>{spec.operator?.channel || 'stable'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Source</DescriptionListTerm>
                    <DescriptionListDescription>{spec.operator?.source || '(platform default)'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Install Plan Approval</DescriptionListTerm>
                    <DescriptionListDescription>{spec.operator?.installPlanApproval || 'Automatic'}</DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={12}>
            <TrustStatusCard
              meshName={mesh.metadata?.name ?? ''}
              meshNamespace={ns}
              issuerName={issuerName ?? ''}
              clusterStatuses={clusterStatuses}
            />
          </GridItem>

          <GridItem span={12}>
            <ClusterStatusSection clusterStatuses={clusterStatuses} />
          </GridItem>

          {conditions.length > 0 && (
            <GridItem span={12}>
              <Card>
                <CardTitle>Conditions</CardTitle>
                <CardBody>
                  <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid">
                    <thead className="pf-v6-c-table__thead">
                      <tr className="pf-v6-c-table__tr">
                        <th className="pf-v6-c-table__th">Type</th>
                        <th className="pf-v6-c-table__th">Status</th>
                        <th className="pf-v6-c-table__th">Reason</th>
                        <th className="pf-v6-c-table__th">Message</th>
                        <th className="pf-v6-c-table__th">Last Transition</th>
                      </tr>
                    </thead>
                    <tbody className="pf-v6-c-table__tbody">
                      {conditions.map((c, i) => (
                        <tr className="pf-v6-c-table__tr" key={`${c.type}-${i}`}>
                          <td className="pf-v6-c-table__td">{c.type}</td>
                          <td className="pf-v6-c-table__td">{statusIcon(c.status)}</td>
                          <td className="pf-v6-c-table__td">{c.reason ?? '-'}</td>
                          <td className="pf-v6-c-table__td">{c.message ?? '-'}</td>
                          <td className="pf-v6-c-table__td">
                            <Timestamp timestamp={c.lastTransitionTime} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </CardBody>
              </Card>
            </GridItem>
          )}
        </Grid>
      </PageSection>
    </>
  )
}

const MeshDetailPage: React.FC = () => {
  const { ns, name } = useParams<{ ns: string; name: string }>()

  if (!ns || !name) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">Not Found</Title>
          <EmptyStateBody>Invalid mesh URL. Expected /service-mesh/:namespace/:name.</EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  return <MeshDetailContent ns={ns} name={name} />
}

export default MeshDetailPage
