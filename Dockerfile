# using registry.ci.openshift.org instead of brew.registry.redhat.io to avoid authorization
FROM registry.ci.openshift.org/stolostron/builder:go1.25-linux AS builder
WORKDIR /go/src/github.com/stolostron/multicluster-mesh-addon
COPY . .
RUN make build

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

LABEL com.redhat.component="rhacm-multicluster-mesh-addon" \
      url="https://github.com/stolostron/multicluster-mesh-addon" \
      description="This image provides the Multicluster Mesh addon, which integrates with Red Hat Advanced Cluster Management (ACM) to orchestrate multi-cluster Istio service mesh deployments." \
      io.k8s.description="Provides multi-cluster service mesh capabilities as an addon managed by Red Hat Advanced Cluster Management." \
      io.k8s.display-name="Red Hat ACM Multicluster Mesh Addon" \
      io.openshift.tags="acm,mesh,servicemesh,istio,multicluster,redhat" \
      name="multicluster-mesh-addon-acm" \
      summary="Multicluster Mesh addon for Red Hat Advanced Cluster Management enabling cross-cluster service mesh orchestration."

COPY LICENSE /licenses/LICENSE
COPY --from=builder /go/src/github.com/stolostron/multicluster-mesh-addon/bin/multicluster-mesh-addon /

USER 1001
