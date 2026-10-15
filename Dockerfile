FROM node:24-alpine AS web

WORKDIR /web
RUN npm install --global pnpm@11.15.1
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
RUN mkdir -p /out /data/cache \
    && CGO_ENABLED=0 go build -trimpath -tags embedweb -o /out/server ./cmd/server \
    && CGO_ENABLED=0 go build -trimpath -o /out/import ./cmd/import \
    && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate \
    && chown -R 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /out/import /out/migrate /usr/local/bin/
COPY --from=build --chown=65532:65532 /data /data

ENV HTTP_ADDR=0.0.0.0:8080 \
    XDG_CACHE_HOME=/data/cache \
    MIGRATE_ON_START=true

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
