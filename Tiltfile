# Build API dev environment with Postgres
# Credentials: builderhub / builderhub123 (dev only)
# build-api runs as local binary; Postgres in cluster with port-forward

# Postgres - plain Deployment, no operator
k8s_yaml(read_file('local-k8s/postgres.yaml'))

# UI grouping - attach the PVC to the main postgres workload resource
k8s_resource(
    'postgres',
    labels=['backend'],
    objects=['postgres-data:persistentvolumeclaim'],
)

# Migrations - manual action (requires migrate CLI from nix dev shell)
local_resource(
    'build-api-migrate',
    cmd='DATABASE_URL="postgres://builderhub:builderhub123@localhost:5432/builderhub?sslmode=disable" make migrate-up',
    deps=['migrations'],
    resource_deps=['postgres-port-forward'],
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['backend'],
)

# Port-forward Postgres for local build-api
local_resource(
    'postgres-port-forward',
    serve_cmd='kubectl wait --for=condition=ready pod -l app=postgres --timeout=120s && kubectl port-forward svc/postgres 5432:5432',
    resource_deps=['postgres'],
    readiness_probe=probe(period_secs=2, tcp_socket=tcp_socket_action(port=5432)),
    allow_parallel=True,
    labels=['backend'],
)

# Build and run build-api as local binary
local_resource(
    'build-api',
    cmd='make build',
    serve_cmd='HTTP_ADDR=:8090 DATABASE_URL="postgres://builderhub:builderhub123@localhost:5432/builderhub?sslmode=disable" JWT_SECRET="dev-secret-change-in-production" ./bin/server',
    deps=['cmd/server', 'internal', 'api/gen', 'migrations'],
    ignore=['bin'],
    resource_deps=['postgres-port-forward'],
    allow_parallel=True,
    labels=['backend'],
)
