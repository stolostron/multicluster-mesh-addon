import { useEffect, useMemo, useState } from 'react'
import type { FC } from 'react'
import { useParams, Link } from 'react-router-dom-v5-compat'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
import {
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
} from '@patternfly/react-core'
import type { Istio } from '../types/istio'
import { istioModel } from '../types/istio'
import { getFromEnrichmentCache, setInEnrichmentCache } from '../hooks/useEnrichedControlPlanes'
import { useMultiClusterMeshes } from '../hooks/useMultiClusterMeshes'
import { buildMcmIndex, lookupMcm } from '../utils/correlateMCM'
import { clusterDetailLink } from '../utils/linkUtils'
import { ConditionsTable } from './ConditionsTable'
import { MeshStatus } from './MeshStatus'
import { CP_TYPES } from '../utils/cpTypeSegment'
import type { CpType } from '../utils/cpTypeSegment'
import { useMeshTranslation } from '../utils/i18nUtils'

const ControlPlaneDetailContent: FC<{ cluster: string; name: string; type: CpType }> = ({ cluster, name, type }) => {
  const { t } = useMeshTranslation()
  const [istio, setIstio] = useState<Istio | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [refreshError, setRefreshError] = useState<unknown>(null)
  const [mcms] = useMultiClusterMeshes()
  const mcmIndex = useMemo(() => buildMcmIndex(mcms ?? []), [mcms])

  useEffect(() => {
    let cancelled = false
    const cached = getFromEnrichmentCache(cluster, name)
    if (cached) {
      setIstio(cached)
      setLoaded(true)
    } else {
      setIstio(null)
      setLoaded(false)
    }
    setError(null)
    setRefreshError(null)

    fleetK8sGet<Istio>({ model: istioModel, name, cluster })
      .then((r) => {
        setInEnrichmentCache(cluster, name, r)
        if (!cancelled) { setIstio(r); setLoaded(true) }
      })
      .catch((e) => {
        if (!cancelled) {
          console.error('Failed to load control plane:', e)
          if (!cached) { setError(e); setLoaded(true) }
          else setRefreshError(e)
        }
      })
    return () => { cancelled = true }
  }, [cluster, name])

  if (!loaded) {
    return (
      <PageSection>
        <Spinner aria-label={t('Loading control plane')} />
      </PageSection>
    )
  }

  if (error) {
    const err = error as Record<string, any>
    const is404 = err?.response?.status === 404 || err?.statusCode === 404 || err?.code === 404
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">
            {is404 ? t('Control plane not found') : t('Error loading control plane')}
          </Title>
          <EmptyStateBody>
            {is404
              ? t('Istio "{{name}}" was not found on cluster "{{cluster}}".', { name, cluster })
              : t('An unexpected error occurred. Check the browser console for details.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  if (!istio) return null
  const spec = istio.spec
  const conditions = istio.status?.conditions ?? []
  const meshID = spec.values?.global?.meshID
  const network = spec.values?.global?.network
  const multiClusterName = spec.values?.global?.multiCluster?.clusterName
  const matchedMCM = lookupMcm(mcmIndex, cluster, spec.namespace)

  return (
    <>
      <PageSection>
        <Breadcrumb>
          <BreadcrumbItem>
            <Link to="/fleet-mesh/control-planes">{t('Control Planes')}</Link>
          </BreadcrumbItem>
          <BreadcrumbItem>{({ managed: t('Managed'), discovered: t('Discovered'), standalone: t('Standalone') })[type]}</BreadcrumbItem>
          <BreadcrumbItem isActive>{`${cluster} / ${name}`}</BreadcrumbItem>
        </Breadcrumb>
        <Flex alignItems={{ default: 'alignItemsCenter' }} style={{ marginTop: '1rem' }}>
          <FlexItem>
            <Title headingLevel="h1">{name}</Title>
          </FlexItem>
          <FlexItem>
            {conditions.length > 0 ? (
              <MeshStatus conditions={conditions} conditionType="Ready" />
            ) : (
              <Label color="grey">{t('Unknown')}</Label>
            )}
          </FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        <Grid hasGutter>
          {!!refreshError && (
            <GridItem span={12}>
              <Alert variant="warning" isInline title={t('Data may be stale — background refresh failed.')} />
            </GridItem>
          )}
          <GridItem span={5}>
            <Card isCompact>
              <CardBody>
                <DescriptionList isCompact columnModifier={{ default: '2Col' }}>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Mesh ID')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      {meshID
                        ? (matchedMCM
                            ? <Link to={`/fleet-mesh/meshes/managed/${encodeURIComponent(matchedMCM.namespace)}/${encodeURIComponent(matchedMCM.name)}`}>{meshID}</Link>
                            : <Link to={`/fleet-mesh/meshes/discovered/${encodeURIComponent(meshID)}`}>{meshID}</Link>)
                        : '-'}
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Network')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{network ?? '-'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Cluster')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      <Link to={clusterDetailLink(cluster)}>
                        {cluster}
                      </Link>
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Control Plane Namespace')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{spec.namespace}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Version')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>{spec.version ?? '-'}</DescriptionListDescription>
                  </DescriptionListGroup>
                  <DescriptionListGroup>
                    <DescriptionListTerm><strong>{t('Created')}</strong></DescriptionListTerm>
                    <DescriptionListDescription>
                      <Timestamp timestamp={istio.metadata?.creationTimestamp} />
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  {multiClusterName && (
                    <DescriptionListGroup>
                      <DescriptionListTerm><strong>{t('Cluster Name (Istio)')}</strong></DescriptionListTerm>
                      <DescriptionListDescription>{multiClusterName}</DescriptionListDescription>
                    </DescriptionListGroup>
                  )}
                </DescriptionList>
              </CardBody>
            </Card>
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

const ControlPlaneDetailPage: FC = () => {
  const { t } = useMeshTranslation()
  const { type, cluster, name } = useParams<{ type: string; cluster: string; name: string }>()

  if (!type || !cluster || !name || !CP_TYPES.includes(type as CpType)) {
    return (
      <PageSection>
        <EmptyState>
          <Title headingLevel="h2" size="lg">{t('Not Found')}</Title>
          <EmptyStateBody>
            {t('Invalid URL. Expected /fleet-mesh/control-planes/:type/:cluster/:name.')}
          </EmptyStateBody>
        </EmptyState>
      </PageSection>
    )
  }

  return <ControlPlaneDetailContent cluster={cluster} name={name} type={type as CpType} />
}

/** Detail page for a single Istio control plane, reached via /fleet-mesh/control-planes/:type/:cluster/:name. */
export default ControlPlaneDetailPage
