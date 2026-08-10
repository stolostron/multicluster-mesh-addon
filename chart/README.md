# Multicluster Mesh Addon Helm Chart

OCM Multi-Cluster Mesh Add-on for orchestrating Istio service mesh deployments across multiple clusters.

## Prerequisites

- Kubernetes cluster with [Open Cluster Management (OCM)](https://open-cluster-management.io/) installed
- Helm 3.x
- At least one managed cluster registered with the hub

## Installation

### Add Helm Repository

```bash
helm repo add multicluster-mesh-addon https://stolostron.github.io/multicluster-mesh-addon
helm repo update
```

### Install the Chart

```bash
helm install multicluster-mesh-addon multicluster-mesh-addon/multicluster-mesh-addon \
  --namespace multicluster-mesh-system \
  --create-namespace
```

### Install Specific Version

```bash
helm install multicluster-mesh-addon multicluster-mesh-addon/multicluster-mesh-addon \
  --version 0.1.0 \
  --namespace multicluster-mesh-system \
  --create-namespace
```

## Configuration

### Example: Custom Image

```bash
helm install multicluster-mesh-addon multicluster-mesh-addon/multicluster-mesh-addon \
  --set image.repository=quay.io/myorg/multicluster-mesh-addon \
  --set image.tag=v0.1.0 \
  --namespace multicluster-mesh-system \
  --create-namespace
```

### Example: Kind Platform

```bash
helm install multicluster-mesh-addon multicluster-mesh-addon/multicluster-mesh-addon \
  --set platform=kind \
  --set image.pullPolicy=IfNotPresent \
  --namespace multicluster-mesh-system \
  --create-namespace
```

## Upgrading

```bash
helm upgrade multicluster-mesh-addon multicluster-mesh-addon/multicluster-mesh-addon \
  --namespace multicluster-mesh-system
```

## Uninstalling

```bash
helm uninstall multicluster-mesh-addon --namespace multicluster-mesh-system
```

