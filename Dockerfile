ARG GO_VERSION=1.26.0-alpine3.23
ARG ALPINE_VERSION=3.23

# Stage: base — shared Go toolchain and dependencies
FROM golang:${GO_VERSION} AS base
RUN apk add --no-cache build-base bash ffmpeg mesa-va-gallium
WORKDIR /app

# Stage: dev — air hot-reload for development
FROM base AS dev
RUN go install github.com/air-verse/air@latest
EXPOSE 8080
WORKDIR /app/apps/server
CMD ["air", "-c", ".air.toml"]

# Stage: dev-frontend — Vite dev server for development
FROM oven/bun:alpine AS dev-frontend
WORKDIR /app
COPY package.json bun.lock* ./
COPY apps/web/package.json apps/web/
COPY apps/desktop/package.json apps/desktop/
COPY apps/server/package.json apps/server/
COPY packages/contracts/package.json packages/contracts/
COPY packages/shared/package.json packages/shared/
RUN bun install --frozen-lockfile
COPY apps ./apps
COPY packages ./packages
EXPOSE 5173
CMD ["sh", "-c", "bun install && bun run --cwd apps/web dev"]

# Stage: test — test runner
FROM base AS test
ENV CGO_ENABLED=0
COPY apps/server/go.mod apps/server/go.sum ./apps/server/
RUN cd apps/server && go mod download
COPY apps/server/ ./apps/server/
CMD ["cd", "apps/server", "&&", "go", "test", "-v", "./..."]

# Stage: build — compile production binary
FROM base AS build
COPY apps/server/go.mod apps/server/go.sum ./apps/server/
RUN cd apps/server && go mod download
COPY apps/server/ ./apps/server/
RUN cd apps/server && CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/main ./cmd/plum

# Stage: production — minimal runtime image
FROM alpine:${ALPINE_VERSION} AS production
RUN apk add --no-cache ca-certificates ffmpeg && \
    adduser -D -u 10001 nonroot

WORKDIR /
COPY --from=build /app/main .

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/main"]
