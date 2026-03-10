ARG GO_VERSION=1.26.0-alpine3.23
ARG ALPINE_VERSION=3.23

# Stage: base — shared Go toolchain and dependencies
FROM golang:${GO_VERSION} AS base
RUN apk add --no-cache build-base bash ffmpeg
WORKDIR /app

# Stage: dev — air hot-reload for development
FROM base AS dev
RUN go install github.com/air-verse/air@latest
EXPOSE 8080
CMD ["air", "-c", "backend/.air.toml"]

# Stage: test — test runner
FROM base AS test
ENV CGO_ENABLED=0
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY backend/ ./backend/
CMD ["cd", "backend", "&&", "go", "test", "-v", "./..."]

# Stage: build — compile production binary
FROM base AS build
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY backend/ ./backend/
RUN cd backend && CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/main ./cmd/plum

# Stage: production — minimal runtime image
FROM alpine:${ALPINE_VERSION} AS production
RUN apk add --no-cache ca-certificates ffmpeg && \
    adduser -D -u 10001 nonroot

WORKDIR /
COPY --from=build /app/main .

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/main"]
