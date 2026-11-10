# Build context: repository root (see docker-compose.yml).
FROM golang:1.22-alpine AS api
WORKDIR /src/api
COPY api/go.mod api/go.sum ./
RUN go mod download
COPY api/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/calor-api ./cmd/calor-api

FROM node:22-alpine AS tracker
WORKDIR /src/tracker
COPY tracker/package.json tracker/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY tracker/ ./
RUN npm run build

FROM alpine:3.20
RUN adduser -D -H calor
COPY --from=api /out/calor-api /usr/local/bin/calor-api
COPY --from=tracker /src/tracker/dist /srv/tracker
# Set CALOR_DATABASE_URL (and PGPASSWORD) at run time; see docker-compose.yml.
ENV CALOR_ADDR=:8080 \
    CALOR_TRACKER_DIR=/srv/tracker \
    GOMEMLIMIT=192MiB
USER calor
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/calor-api"]
