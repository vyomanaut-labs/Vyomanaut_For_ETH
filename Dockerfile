# Dockerfile (repo root): the whole network in one container (coordinator, relay and
# local providers), for hosting on Railway. Build context = the repo root.
# Postgres is NOT in here: use Railway's Postgres service (guides/deploy_railway.md).
FROM golang:1.26-bookworm

RUN apt-get update \
 && apt-get install -y --no-install-recommends postgresql-client openssl curl ca-certificates procps \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# Only what the binaries need comes first, so a change to a script or a guide does not recompile.
COPY cmd cmd
COPY internal internal
COPY migrations migrations
COPY scripts/release scripts/release

# Build everything once, with the pure-Go storage engine (no RocksDB or C toolchain needed), and
# warm the Go cache for the schema generator that start.sh runs. A small container cannot afford
# a compile at start-up.
ENV VYOMANAUT_DEMO_STATE_DIR=/var/vyomanaut
RUN set -eux; \
    eval "$(bash scripts/release/purego_env.sh)"; \
    mkdir -p /var/vyomanaut/bin; \
    for c in microservice provider operator client relay; do \
      go build -o "/var/vyomanaut/bin/$c" "./cmd/$c"; \
    done; \
    go run migrations/generator.go --profile=demo >/dev/null

COPY . .

ENV VYOMANAUT_SKIP_BUILD=1
CMD ["bash", "deployments/railway/start.sh"]
