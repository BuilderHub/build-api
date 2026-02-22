# BuilderHub Build API - local dev
allow_k8s_contexts('kind-kind', 'docker-desktop', 'minikube')

local_resource(
    'build-api',
    cmd='make build',
    deps=['cmd/', 'internal/', 'api/', 'go.mod', 'go.sum', 'Makefile'],
    serve_cmd='bin/build-api --grpc-addr=:9090 --http-addr=:8080',
)
