## Verify Mesh Federation for Istio

1. Deploy part(productpage,details,reviews-v1,reviews-v2,ratings) of the bookinfo application in managed cluster `eks1`:

```bash
kubectl create ns bookinfo
kubectl label namespace bookinfo istio-injection=enabled
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.13/samples/bookinfo/platform/kube/bookinfo.yaml -l 'app,version notin (v3)'
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.13/samples/bookinfo/platform/kube/bookinfo.yaml -l 'account'
```

2. Then deploy another part(reviews-v3, ratings) of bookinfo application in managed cluster `eks2`:

```bash
kubectl create ns bookinfo
kubectl label namespace bookinfo istio-injection=enabled
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.8/samples/bookinfo/platform/kube/bookinfo.yaml -l 'app,version in (v3)' 
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.8/samples/bookinfo/platform/kube/bookinfo.yaml -l 'service=reviews' 
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.8/samples/bookinfo/platform/kube/bookinfo.yaml -l 'account=reviews' 
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.8/samples/bookinfo/platform/kube/bookinfo.yaml -l 'app=ratings' 
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.8/samples/bookinfo/platform/kube/bookinfo.yaml -l 'account=ratings'
```

3. Get the the network load balancer IP of eastwest gateway from mesh `eks2-istio`:

_Note:_ Make sure the cloud provider support the network load balancer IP before proceeding.

```bash
EASTWAST_GW_IP=$(kubectl -n istio-system get svc istio-eastwestgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

4. Create the following `serviceentry` in managed cluster `eks1` to discovery service(reviews-v3) from mesh `eks2-istio` with the IP retrieved from the last step:

```bash
cat << EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: reviews.bookinfo.svc.cluster2.global
  namespace: istio-system
spec:
  addresses:
  - 255.51.210.11
  endpoints:
  - address: ${EASTWAST_GW_IP}
    labels:
      app: reviews
      version: v3
    ports:
      http: 15443
  hosts:
  - reviews.bookinfo.svc.cluster2.global
  location: MESH_INTERNAL
  ports:
  - name: http
    number: 9080
    protocol: HTTP
  resolution: STATIC
EOF
```

5. Create the following `serviceentry` and `destinationrule` resources in managed cluster `eks2` to expose service(reviews-v3) in mesh `eks2-istio`:

```bash
REVIEW_V3_IP=$(kubectl -n bookinfo get pod -l app=reviews -o jsonpath="{.items[0].status.podIP}")
cat << EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: reviews.bookinfo.svc.cluster2.global
  namespace: istio-system
spec:
  endpoints:
  - address: ${REVIEW_V3_IP}
    labels:
      app: reviews
      version: v3
    ports:
      http: 9080
  exportTo:
  - .
  hosts:
  - reviews.bookinfo.svc.cluster2.global
  location: MESH_INTERNAL
  ports:
  - name: http
    number: 9080
    protocol: HTTP
  resolution: STATIC
---
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: reviews-bookinfo-cluster2
  namespace: istio-system
spec:
  host: reviews.bookinfo.svc.cluster2.global
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
EOF
```

6. Create the following `virtualservice` and `destinationrule` resources in managed cluster `eks1` to route traffic from mesh `eks1-istio` to mesh `eks2-istio`:

```bash
cat << EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: reviews
  namespace: bookinfo
spec:
  hosts:
  - reviews.bookinfo.svc.cluster.local
  http:
  - match:
    - port: 9080
    route:
    - destination:
        host: reviews.bookinfo.svc.cluster2.global
        port:
          number: 9080
      weight: 75
    - destination:
        host: reviews.bookinfo.svc.cluster.local
        port:
          number: 9080
        subset: version-v1
      weight: 15
    - destination:
        host: reviews.bookinfo.svc.cluster.local
        port:
          number: 9080
        subset: version-v2
      weight: 10
---
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: reviews
  namespace: bookinfo
spec:
  host: reviews.bookinfo.svc.cluster.local
  subsets:
  - labels:
      version: v1
    name: version-v1
  - labels:
      version: v2
    name: version-v2
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
---
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: reviews-bookinfo-cluster2
  namespace: istio-system
spec:
  host: reviews.bookinfo.svc.cluster2.global
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
EOF
```

7. Access the bookinfo from your browser with the following address from `eks1` cluster:

```bash
INGRESS_GW_IP=$(kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo http://${INGRESS_GW_IP}/productpage
```

_Note_: The expected result is that by refreshing the page several times, you should occasionally see traffic being routed to the `reviews-v3` service, which will produce red-colored stars on the product page.
