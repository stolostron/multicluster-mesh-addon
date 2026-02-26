FROM registry.ci.openshift.org/stolostron/builder:go1.25-linux AS builder
WORKDIR /workspace
COPY . .
USER 0
RUN make build

FROM scratch
COPY --from=builder /workspace/bin/multicluster-mesh-addon /
USER 65532:65532

ENTRYPOINT ["/multicluster-mesh-addon"]
