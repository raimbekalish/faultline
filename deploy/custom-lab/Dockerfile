# PREPARATION ONLY: no build/pull/run was performed or authorized in M8B2C.
# Worker intentionally supports preflight only; this is NOT a qualified catalog.
FROM golang:1.26.8-bookworm@sha256:dc9ad6c05acc7a88e5b71bde60a5fe3bd4b9f0db209011711b464107438a8107 AS build
ARG SOURCE_REVISION
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOTOOLCHAIN=local
COPY go.mod go.sum ./
# spec reuses the existing engine's numeric validation. Its compile-time closure
# includes the packages below and locked Go modules, even for preflight-only use.
RUN go mod download
COPY internal/spec ./internal/spec
COPY internal/experiment ./internal/experiment
COPY internal/localhttp ./internal/localhttp
COPY internal/provenance ./internal/provenance
COPY internal/telemetry ./internal/telemetry
COPY internal/toxiproxy ./internal/toxiproxy
COPY internal/jsonstrict ./internal/jsonstrict
COPY internal/labprotocol ./internal/labprotocol
COPY cmd/lab-worker ./cmd/lab-worker
COPY cmd/lab-fixture ./cmd/lab-fixture
RUN test "${#SOURCE_REVISION}" -eq 40 && ! printf '%s' "$SOURCE_REVISION" | LC_ALL=C grep -q '[^0-9a-f]' \
 && GOPROXY=off go build -buildvcs=false -trimpath -ldflags="-s -w -X main.revision=${SOURCE_REVISION}" -o /out/faultline ./cmd/lab-worker \
 && GOPROXY=off go build -buildvcs=false -trimpath -ldflags='-s -w' -o /out/sample ./cmd/lab-fixture
FROM scratch
ARG SOURCE_REVISION
LABEL com.faultline.lab.contract="http-work-v1" org.opencontainers.image.revision="${SOURCE_REVISION}"
COPY --from=build /out/ /app/
USER 65532:65532
WORKDIR /
STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/faultline"]
CMD ["lab-worker"]
