# Build API dev environment with CNPG (CloudNativePG) PostgreSQL
# Hardcoded credentials: builderhub / builderhub123 (dev only)

# CNPG operator
load('ext://helm_remote', 'helm_remote')
helm_remote(
    'cloudnative-pg',
    repo_url='https://cloudnative-pg.github.io/charts',
    repo_name='cloudnative-pg',
    namespace='cnpg-system',
    create_namespace=True,
)

# CNPG cluster with hardcoded dev credentials (builderhub / builderhub123)
k8s_yaml([
    read_file('local-k8s/cnpg-secret.yaml'),
    read_file('local-k8s/cnpg-cluster.yaml'),
])

# Port-forward Postgres for local build-api
k8s_resource(
    'builderhub-rw',
    port_forwards=['5432:5432'],
)

# Build and run build-api locally (run "make migrate-up" after postgres is ready)
local_resource(
    'build-api',
    cmd='make build && DATABASE_URL="postgres://builderhub:builderhub123@localhost:5432/builderhub?sslmode=disable" JWT_SECRET="dev-secret-change-in-production" ./bin/server',
    deps=['cmd/server', 'internal', 'api/gen', 'migrations'],
    resource_deps=['builderhub-rw'],
    auto_init=True,
)
