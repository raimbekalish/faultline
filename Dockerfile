FROM golang:1.26.8-bookworm@sha256:dc9ad6c05acc7a88e5b71bde60a5fe3bd4b9f0db209011711b464107438a8107 AS dev
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=1
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["go", "test", "-race", "./..."]

FROM dev AS build
ARG REVISION=development
RUN CGO_ENABLED=1 go build -tags netgo,osusergo -trimpath -ldflags="-s -w -linkmode external -extldflags '-static' -X main.revision=${REVISION}" -o /out/faultline ./cmd/faultline \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sample ./cmd/sample

FROM scratch AS runtime
COPY --from=build /out/ /app/
USER 65532:65532
ENTRYPOINT ["/app/faultline"]
