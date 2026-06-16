import * as React from 'react'
import { useParams, Link } from 'react-router-dom-v5-compat'
import {
  useK8sWatchResource,
  Timestamp,
} from '@openshift-console/dynamic-plugin-sdk'
import {
  Breadcrumb,
  BreadcrumbItem,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  EmptyState,
  EmptyStateBody,
  PageSection,
  Spinner,
  Title,
} from '@patternfly/react-core'
import type { MultiClusterMesh, K8sCondition } from '../types/multiClusterMesh'
import { multiClusterMeshGroupVersionKind } from '../types/multiClusterMesh'
import { MeshStatus } from './MeshStatus'

function conditionMessage(condition: K8sCondition): string {
  if (condition.message) return condition.message
  if (condition.reason) return condition.reason
  return condition.status === 'True' ? 'True' : 'False'
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
        <Title headingLevel="h1" style={{ marginTop: '1rem' }}>
          {mesh.metadata?.name}{' '}
          <MeshStatus conditions={conditions} conditionType="Ready" />
        </Title>
      </PageSection>

      <PageSection>
        <Title headingLevel="h2" size="lg" style={{ marginBottom: '1rem' }}>Overview</Title>
        <DescriptionList isHorizontal>
          <DescriptionListGroup>
            <DescriptionListTerm>Cluster Set</DescriptionListTerm>
            <DescriptionListDescription>{spec.clusterSet}</DescriptionListDescription>
          </DescriptionListGroup>
          <DescriptionListGroup>
            <DescriptionListTerm>Control Plane Namespace</DescriptionListTerm>
            <DescriptionListDescription>{spec.controlPlane?.namespace || 'istio-system'}</DescriptionListDescription>
          </DescriptionListGroup>
          <DescriptionListGroup>
            <DescriptionListTerm>Operator Namespace</DescriptionListTerm>
            <DescriptionListDescription>{spec.operator?.namespace || '(platform default)'}</DescriptionListDescription>
          </DescriptionListGroup>
          <DescriptionListGroup>
            <DescriptionListTerm>Operator Channel</DescriptionListTerm>
            <DescriptionListDescription>{spec.operator?.channel || 'stable'}</DescriptionListDescription>
          </DescriptionListGroup>
          <DescriptionListGroup>
            <DescriptionListTerm>Operator Source</DescriptionListTerm>
            <DescriptionListDescription>{spec.operator?.source || '(platform default)'}</DescriptionListDescription>
          </DescriptionListGroup>
          <DescriptionListGroup>
            <DescriptionListTerm>Install Plan Approval</DescriptionListTerm>
            <DescriptionListDescription>{spec.operator?.installPlanApproval || 'Automatic'}</DescriptionListDescription>
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
      </PageSection>

      <PageSection>
        <Title headingLevel="h2" size="lg" style={{ marginBottom: '1rem' }}>
          Cluster Status ({clusterStatuses.length})
        </Title>
        {clusterStatuses.length === 0 ? (
          <EmptyState>
            <EmptyStateBody>No clusters are part of this mesh yet.</EmptyStateBody>
          </EmptyState>
        ) : (
          <table className="pf-v6-c-table pf-m-grid-md" role="grid">
            <thead>
              <tr>
                <th>Cluster</th>
                <th>Operator Status</th>
                <th>Message</th>
              </tr>
            </thead>
            <tbody>
              {clusterStatuses.map((cs) => {
                const operatorCondition = cs.conditions?.find((c) => c.type === 'OperatorInstalled')
                return (
                  <tr key={cs.clusterName}>
                    <td>{cs.clusterName}</td>
                    <td>
                      <MeshStatus conditions={cs.conditions} conditionType="OperatorInstalled" />
                    </td>
                    <td>{operatorCondition ? conditionMessage(operatorCondition) : '-'}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </PageSection>

      {conditions.length > 0 && (
        <PageSection>
          <Title headingLevel="h2" size="lg" style={{ marginBottom: '1rem' }}>Conditions</Title>
          <table className="pf-v6-c-table pf-m-grid-md" role="grid">
            <thead>
              <tr>
                <th>Type</th>
                <th>Status</th>
                <th>Reason</th>
                <th>Message</th>
                <th>Last Transition</th>
              </tr>
            </thead>
            <tbody>
              {conditions.map((c, i) => (
                <tr key={`${c.type}-${i}`}>
                  <td>{c.type}</td>
                  <td>{c.status}</td>
                  <td>{c.reason ?? '-'}</td>
                  <td>{c.message ?? '-'}</td>
                  <td>
                    <Timestamp timestamp={c.lastTransitionTime} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </PageSection>
      )}
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
