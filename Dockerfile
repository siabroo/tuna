# syntax=docker/dockerfile:1.7

# Build stage
FROM golang:1.26 AS build
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

# CGO_ENABLED=0 keeps the binary static
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /workspace/bin/manager ./cmd/manager

# Runtime stage — distroless static (no shell, no package manager)
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /workspace/bin/manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
