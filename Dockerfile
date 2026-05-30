# BuilderHub Build API - multi-stage, distroless, multi-arch

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
WORKDIR /workspace

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-w -s" -o build-api ./cmd/server

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/build-api .
USER nonroot:nonroot
ENTRYPOINT ["/build-api"]
