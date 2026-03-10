ARG BUN_VERSION=1.3.10
ARG NGINX_VERSION=1.28.2-alpine3.23

# Stage: deps — install monorepo workspaces
FROM oven/bun:${BUN_VERSION} AS deps
WORKDIR /app

# Copy monorepo root manifest and workspaces so Bun can resolve workspace:* deps
COPY package.json bun.lock* ./
COPY packages ./packages
COPY apps/web/package.json apps/web/bun.lock* ./apps/web/

RUN if [ -f bun.lock ]; then bun install --frozen-lockfile; else bun install; fi

# Stage: dev — vite dev server
FROM deps AS dev
WORKDIR /app/apps/web
EXPOSE 5173
CMD ["bun", "run", "dev", "--host"]

# Stage: build — vite production build
FROM deps AS build
WORKDIR /app
COPY . .
WORKDIR /app/apps/web
RUN bun run build

# Stage: production — nginx static serving
FROM nginx:${NGINX_VERSION} AS production
COPY --from=build /app/apps/web/dist /usr/share/nginx/html
COPY apps/web/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
