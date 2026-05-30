# Build API

BuilderHub public-facing API. Manages BuildkitBuilder CRs via gRPC and REST.

## Features

- **gRPC** on port 9090 (builders, organizations, templates, auth, health)
- **REST** via grpc-gateway on port 8080
- **Swagger UI** at `/docs`, OpenAPI spec at `/openapi.json`
- **Health** endpoints at `/health` and `/ready`

## Prerequisites

- Go 1.25+
- buf
- Kubernetes cluster with [build-operator](https://github.com/builderhub/build-operator) CRDs installed

## Local development

```bash
# Nix (recommended)
nix develop
make build
make run

# Or without Nix
make build
./bin/build-api --grpc-addr=:9090 --http-addr=:8080 --kubeconfig-path=
```

Set `GOPRIVATE=github.com/builderhub/*` and configure `~/.netrc` for private module access.

## Make targets

| Target | Description |
|--------|-------------|
| `make generate` | Generate proto code (buf dep update + buf generate) |
| `make build` | Build binary |
| `make run` | Build and run server |
| `make migrate-up` | Run DB migrations (requires DATABASE_URL) |
| `make migrate-down` | Rollback DB migrations |

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /v1/namespaces/{ns}/builders | List builders |
| GET | /v1/namespaces/{ns}/builders/{name} | Get builder |
| POST | /v1/namespaces/{ns}/builders | Create builder |
| POST | /v1/namespaces/{ns}/builders/{name}/wake | Wake sleepy builder |
| GET | /v1/namespaces/{ns}/templates | List builder templates (org/namespace-scoped) |
| GET | /v1/namespaces/{ns}/templates/{name} | Get builder template |
| POST | /v1/namespaces/{ns}/templates | Create builder template |
| PATCH | /v1/namespaces/{ns}/templates/{name} | Update builder template |
| DELETE | /v1/namespaces/{ns}/templates/{name} | Delete builder template |
| GET | /v1/health | Health check |
| GET | /docs | Swagger UI |
| GET | /openapi.json | OpenAPI spec |

## Deployment

### Docker

```bash
docker buildx build -f Dockerfile .
```

### Helm

```bash
helm install build-api helm/build-api -n builderhub --create-namespace \
  --set image.repository=ghcr.io/builderhub/build-api \
  --set image.tag=v0.0.0-beta.0
```

## Scopes for API keys

When creating API keys (via AuthService), the following scopes are available:

- `builders:read`, `builders:write`
- `organizations:read`, `organizations:write`
- `templates:read`, `templates:write` (new — for managing BuildkitBuilderTemplates)

JWT sessions from `auth login` have full access.

## License

This project is licensed under the [MIT License](LICENSE).
