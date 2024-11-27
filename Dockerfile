FROM golang:1.23.2-alpine AS build

RUN go env -w GOCACHE=/go-cache
RUN go env -w GOMODCACHE=/gomod-cache

WORKDIR /go/src/bb-noti
COPY . .
RUN --mount=type=cache,target=/gomod-cache --mount=type=cache,target=/go-cache \
    CGO_ENABLED=0 go build -o /go/bin/bb-noti ./cmd/noti
RUN GRPC_HEALTH_PROBE_VERSION=v0.4.35 && \
    wget -qO /go/bin/grpc_health_probe \
    https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 && \
    chmod +x /go/bin/grpc_health_probe

FROM alpine
COPY --from=build /go/bin/bb-noti /bin/bb-noti
COPY --from=build /go/bin/grpc_health_probe /bin/grpc_health_probe
ENTRYPOINT ["/bin/bb-noti"]