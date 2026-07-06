import { useEffect, useMemo } from 'react'
import type { FC } from 'react'
import { useParams, Link } from 'react-router-dom-v5-compat'
import {
  useK8sWatchResource,
  Timestamp,
} from '@openshift-console/dynamic-plugin-sdk'
import {
  Alert,
  Breadcrumb,
  BreadcrumbItem,
  Card,
  CardBody,
  CardTitle,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Divider,
  EmptyState,
  EmptyStateBody,
  Flex,
  FlexItem,
  Grid,
  GridItem,
  Label,
  PageSection,
  Spinner,
  Title,
  Tooltip,
} from '@patternfly/react-core'
import type { MultiClusterMesh, ClusterMeshStatus } from '../types/multiClusterMesh'
import type { K8sCondition } from '../types/common'
import { multiClusterMeshGroupVersionKind } from '../types/multiClusterMesh'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import { useMeshControlPlanes } from '../hooks/useMeshControlPlanes'
import { useManagedClusterMap } from '../hooks/useManagedClusterMap'
import type { ManagedCluster } from '../types/managedCluster'
import { getClusterAvailability, availabilityColor, availabilityLabelKey } from '../types/managedCluster'
import { clusterDetailLink, clusterSetDetailLink } from '../utils/linkUtils'
import { ConditionsTable } from './ConditionsTable'
import { ControlPlanesCard } from './ControlPlanesCard'
import { MeshStatus } from './MeshStatus'
import { TrustStatusCard } from './TrustStatusCard'
import { VirtualFilterTable } from './VirtualFilterTable'
import type { CategoryLabel, VirtualFilterColumn } from './VirtualFilterTable'
import { useMeshTranslation } from '../utils/i18nUtils'
import { clusterMeshStatusRowKey, clusterMeshStatusSearchMatch } from '../utils/tableCallbacks'

function conditionMessage(condition: K8sCondition): string {
  if (condition.message) return condition.message
  if (condition.reason) return condition.reason
  return condition.status
}

type ClusterCategory = 'ready' | 'notReady' | 'unknown'

function categorizeCluster(cs: ClusterMeshStatus): ClusterCategory {
  const op = cs.conditions?.find((c) => c.type === 'OperatorInstalled')
  if (!op) return 'unknown'
  if (op.status === 'True') return 'ready'
  if (op.status === 'Unknown') return 'unknown'
  return 'notReady'
}

const CONFLICT_REASONS = ['OperatorConfigConflict', 'NamespaceConflict']

const CLUSTER_CATEGORY_LABELS: CategoryLabel[] = [
  { key: 'all', label: 'All ({{count}})' },
  { key: 'ready', label: 'Ready ({{count}})' },
  { key: 'notReady', label: 'Not Ready ({{count}})' },
  { key: 'unknown', label: 'Unknown ({{count}})' },
]

/** Per-cluster operator status table with filter toggles and search for a single mesh. */
export const ClusterStatusSection: FC<{
  clusterStatuses: ClusterMeshStatus[]
  managedClusterMap?: Map<string, ManagedCluster>
  managedClustersLoaded?: boolean
  meshConditions?: K8sCondition[]
}> = ({
  clusterStatuses,
  managedClusterMap,
  managedClustersLoaded = true,
  meshConditions,
}) => {
  const { t } = useMeshTranslation()

  const columns = useMemo<VirtualFilterColumn<ClusterMeshStatus>[]>(() => [
    {
      key: 'cluster',
      label: 'Cluster',
      render: (cs) => (
        <Link to={clusterDetailLink(cs.clusterName)}>{cs.clusterName}</Link>
      ),
      width: '25%',
    },
    {
      key: 'clusterStatus',
      label: 'Cluster Status',
      render: (cs) => {
        if (!managedClustersLoaded) return '-'
        const availability = getClusterAvailability(managedClusterMap?.get(cs.clusterName))
        return <Label color={availabilityColor(availability)} isCompact>{t(availabilityLabelKey(availability))}</Label>
      },
      width: '20%',
    },
    {
      key: 'operatorStatus',
      label: 'Operator Status',
      render: (cs) => <MeshStatus conditions={cs.conditions} conditionType="OperatorInstalled" isCompact />,
      width: '20%',
    },
    {
      key: 'message',
      label: 'Message',
      render: (cs) => {
        const operatorCondition = cs.conditions?.find((c) => c.type === 'OperatorInstalled')
        const msg = operatorCondition ? conditionMessage(operatorCondition) : '-'
        return <Tooltip content={msg}><span>{msg}</span></Tooltip>
      },
      width: '35%',
    },
  ], [managedClusterMap, managedClustersLoaded, t])

  if (clusterStatuses.length === 0) {
    const readyCondition = meshConditions?.find((c) => c.type === 'Ready')
    const isConflict = readyCondition && CONFLICT_REASONS.includes(readyCondition.reason ?? '')
    return (
      <Card isCompact>
        <CardTitle><strong>{t('Clusters (0)')}</strong></CardTitle>
        <CardBody>
          <EmptyState variant="xs">
            <EmptyStateBody>
              {isConflict
                ? t('This mesh is blocked: {{reason}}. Resolve the conflict to allow reconciliation.', {
                    reason: readyCondition?.message || readyCondition?.reason,
                  })
                : t('No clusters are part of this mesh yet.')}
            </EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    )
  }

  return (
    <Card isCompact>
      <CardTitle><strong>{t('Clusters ({{count}})', { count: clusterStatuses.length })}</strong></CardTitle>
      <CardBody>
        <VirtualFilterTable
          categorize={categorizeCluster}
          categoryLabels={CLUSTER_CATEGORY_LABELS}
          columns={columns}
          emptyMessage="No clusters match the current filter."
          items={clusterStatuses}
          rowKey={clusterMeshStatusRowKey}
          searchMatch={clusterMeshStatusSearchMatch}
          searchPlaceholder="Filter by cluster name"
        />
      </CardBody>
    </Card>
  )
}

const MeshDetailContent: FC<{ ns: string; name: string }> = ({ ns, name }) => {
  const { t } = useMeshTranslation()
  const [mesh, loaded, loadError] = useK8sWatchResource<MultiClusterMesh>({
    groupVersionKind: multiClusterMeshGroupVersionKind,
    name,
    namespace: ns,
  })
  const [mcms] = useMultiClusterMeshes()
  const [managedClusterMap, managedClustersLoaded] = useManagedClusterMap()
  const clusterNames = useMemo(
    () => (mesh?.status?.clusterStatus ?? []).map((cs) => cs.clusterName),
    [mesh],
  )
  const [enrichedPlanes, , enrichmentError] = useMeshControlPlanes(clusterNames, mcms ?? [])
  const managedPlanes = useMemo(
    () => enrichedPlanes.filter((cp) => cp.managedBy?.name === name && cp.managedBy?.namespace === ns),
    [enrichedPlanes, name, ns],
  )

  useEffect(() => {
    if (loadError) console.error('Failed to load mesh:', loadError)
  }, [loadError])

  if (loadError) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Error loading mesh')}</Title>
          <EmptyStateBody>
            {t('An unexpected error occurred. Check the browser console for details.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  if (!loaded) {
    return (
      <PageSection>
        <Spinner aria-label={t('Loading mesh details')} />
      </PageSection>
    )
  }

  if (!mesh) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Mesh not found')}</Title>
          <EmptyStateBody>
            {t('MultiClusterMesh "{{name}}" was not found in namespace "{{ns}}".', { name, ns })}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  const spec = mesh.spec
  const status = mesh.status
  const clusterStatuses = status?.clusterStatus ?? []
  const conditions = status?.conditions ?? []
  const issuerRef = spec.security?.trust?.certManager?.issuerRef
  const issuerName = issuerRef?.name

  return (
    <>
      <PageSection>
        <Breadcrumb>
          <BreadcrumbItem>
            <Link to="/fleet-mesh/meshes">{t('Meshes')}</Link>
          </BreadcrumbItem>
          <BreadcrumbItem>{t('Managed')}</BreadcrumbItem>
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
          {!!enrichmentError && (
            <GridItem span={12}>
              <Alert
                variant="warning"
                isInline
                title={t('Unable to load control plane data. Some information may be incomplete.')}
              />
            </GridItem>
          )}

          <GridItem span={12}>
            <Card isCompact>
              <CardBody>
                <DescriptionList isCompact columnModifier={{ default: '2Col' }}>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Mesh ID')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{managedPlanes[0]?.meshID ?? `${ns}-${name}`}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Cluster Set')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      <Link to={clusterSetDetailLink(spec.clusterSet)}>
                        {spec.clusterSet}
                      </Link>
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Control Plane Namespace')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.controlPlane?.namespace || 'istio-system'}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('cert-manager Issuer')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {issuerName
                        ? `${issuerName} (${issuerRef?.kind || 'Issuer'})`
                        : t('Not configured')}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Created')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      <Timestamp timestamp={mesh.metadata?.creationTimestamp} />
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
                <Divider style={{ margin: '0.75rem 0' }} />
                <Title headingLevel="h4" size="md" style={{ marginBottom: '0.5rem' }}>{t('OSSM Operator')}</Title>
                <DescriptionList isCompact columnModifier={{ default: '2Col' }}>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Namespace')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.operator?.namespace || t('(platform default)')}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Channel')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{spec.operator?.channel || 'stable'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Source')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.operator?.source || t('(platform default)')}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Install Plan Approval')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {spec.operator?.installPlanApproval || 'Automatic'}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                </DescriptionList>
              </CardBody>
            </Card>
          </GridItem>

          <GridItem span={12}>
            <ClusterStatusSection clusterStatuses={clusterStatuses} managedClusterMap={managedClusterMap} managedClustersLoaded={managedClustersLoaded} meshConditions={conditions} />
          </GridItem>

          <GridItem span={12}>
            <ControlPlanesCard planes={managedPlanes} />
          </GridItem>

          <GridItem span={12}>
            <TrustStatusCard
              clusterStatuses={clusterStatuses}
              issuerName={issuerName ?? ''}
              meshName={mesh.metadata?.name ?? ''}
              meshNamespace={ns}
            />
          </GridItem>

          {conditions.length > 0 && (
            <GridItem span={12}>
              <Card isCompact>
                <CardTitle><strong>{t('Conditions')}</strong></CardTitle>
                <CardBody>
                  <ConditionsTable conditions={conditions} />
                </CardBody>
              </Card>
            </GridItem>
          )}
        </Grid>
      </PageSection>
    </>
  )
}

const MeshDetailPage: FC = () => {
  const { t } = useMeshTranslation()
  const { ns, name } = useParams<{ ns: string; name: string }>()

  if (!ns || !name) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Not Found')}</Title>
          <EmptyStateBody>
            {t('Invalid mesh URL. Expected /fleet-mesh/meshes/managed/:namespace/:name.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  return <MeshDetailContent ns={ns} name={name} />
}

/** Detail page for a single MultiClusterMesh, reached via /fleet-mesh/meshes/managed/:ns/:name. */
export default MeshDetailPage
