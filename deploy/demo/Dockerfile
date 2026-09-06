FROM node:22-alpine AS build

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./

# NEXT_PUBLIC_* values are compiled into the browser bundle at build time.
ENV NEXT_PUBLIC_MOCK=1
RUN npm run build:static

FROM nginxinc/nginx-unprivileged:1.29-alpine

COPY deploy/demo/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/web/out /usr/share/nginx/html

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/ >/dev/null || exit 1
