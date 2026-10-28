# Independent standard-library contract app; preparation only, not built here.
FROM golang:1.26.8-bookworm@sha256:dc9ad6c05acc7a88e5b71bde60a5fe3bd4b9f0db209011711b464107438a8107 AS build
WORKDIR /src
COPY examples/custom-service/http-work/main.go ./main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOTOOLCHAIN=local GO111MODULE=off go build -buildvcs=false -trimpath -ldflags='-s -w' -o /out/service main.go
FROM scratch
COPY --from=build /out/service /app/service
USER 65532:65532
WORKDIR /
STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/service"]
