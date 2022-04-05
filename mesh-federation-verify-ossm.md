## Verify Mesh Federation for Openshift Service Meshes

1. Deploy part(productpage,details,reviews-v1) of the bookinfo in managed cluster `ocp1`:

```bash
oc create ns bookinfo
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/platform/kube/bookinfo.yaml -l 'app in (productpage,details)'
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/platform/kube/bookinfo.yaml -l app=reviews,version=v1
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/platform/kube/bookinfo.yaml -l service=reviews
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/platform/kube/bookinfo.yaml -l 'account'
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/networking/bookinfo-gateway.yaml
```

2. Then deploy the remaining part(reviews-v2, reviews-v3, ratings) of bookinfo application in managed cluster `ocp2`:

```bash
oc create ns bookinfo
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/platform/kube/bookinfo.yaml -l app=reviews,version=v2
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/platform/kube/bookinfo.yaml -l app=reviews,version=v3
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/platform/kube/bookinfo.yaml -l service=reviews
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/platform/kube/bookinfo.yaml -l app=ratings
oc apply -n bookinfo -f https://raw.githubusercontent.com/maistra/istio/maistra-2.1/samples/bookinfo/platform/kube/bookinfo.yaml -l 'account'
```

3. Create `exportedserviceset` resource in managed cluster `ocp2` to export services(reviews and ratings) from `ocp2-ossm`:

```bash
cat << EOF | oc apply -f -
apiVersion: federation.maistra.io/v1
kind: ExportedServiceSet
metadata:
  name: ocp1-ossm
  namespace: mesh-system
spec:
  exportRules:
  - type: NameSelector
    nameSelector:
      namespace: bookinfo
      name: reviews
  - type: NameSelector
    nameSelector:
      namespace: bookinfo
      name: ratings
EOF
```

4. Create `importedserviceset` resource in managed cluster `ocp1` to import services(reviews and ratings) to `ocp1-ossm`:

```bash
cat << EOF | oc apply -f -
apiVersion: federation.maistra.io/v1
kind: ImportedServiceSet
metadata:
  name: ocp2-ossm
  namespace: mesh-system
spec:
  importRules:
    - type: NameSelector
      importAsLocal: true
      nameSelector:
        namespace: bookinfo
        name: reviews
        alias:
          namespace: bookinfo
    - type: NameSelector
      importAsLocal: true
      nameSelector:
        namespace: bookinfo
        name: ratings
        alias:
          namespace: bookinfo
EOF
```

5. Access the bookinfo from your browser with the following address from `ocp1` cluster:

```bash
echo http://$(oc -n mesh-system get route istio-ingressgateway -o jsonpath={.spec.host})/productpage
```

_Note_: The expected result is that by refreshing the page several times, you should see different versions of reviews shown in productpage, presented in a round robin style (red stars, black stars, no stars). Because reviews-v2, reviews-v3 and ratings service are running in another mesh, if you could see black stars and red stars reviews, then it means traffic across meshes are successfully routed.
