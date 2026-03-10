ARG BUN_VERSION=1.3.10
ARG NGINX_VERSION=1.28.2-alpine3.23

# Stage: deps — install node_modules
FROM oven/bun:${BUN_VERSION} AS deps
WORKDIR /app
COPY package.json bun.lock* ./
RUN if [ -f bun.lock ]; then bun install --frozen-lockfile; else bun install; fi

# Stage: dev — vite dev server
FROM deps AS dev
WORKDIR /app
EXPOSE 5173
CMD ["bun", "run", "dev", "--host"]

# Stage: build — vite production build
FROM deps AS build
COPY . .
RUN bun run build

# Stage: production — nginx static serving
FROM nginx:${NGINX_VERSION} AS production
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
