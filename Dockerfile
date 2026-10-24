FROM golang:1.26.8-bookworm@sha256:dc9ad6c05acc7a88e5b71bde60a5fe3bd4b9f0db209011711b464107438a8107 AS dev
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=1
COPY go.mod ./
COPY . .
CMD ["go", "test", "-race", "./..."]
