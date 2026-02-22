# BuilderHub Build API - multi-stage, distroless, multi-arch
# Requires netrc for go mod download (private build-operator): --secret id=netrc,env=SECRET_SLOT_0
# Build from build-api repo: docker buildx build --secret id=netrc,env=SECRET_SLOT_0 .

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
WORKDIR /workspace

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
COPY . .

# go mod download fetches build-operator from github.com/builderhub/build-operator (netrc for private repo auth)
ENV GOPRIVATE=github.com/builderhub/*
RUN --mount=type=secret,id=netrc,target=/root/.netrc go mod download

ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-w -s" -o build-api ./cmd/server

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/build-api .
USER nonroot:nonroot
ENTRYPOINT ["/build-api"]
