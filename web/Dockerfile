ARG NODE_IMAGE=node:22-alpine
ARG NGINX_IMAGE=nginx:alpine
FROM ${NODE_IMAGE} AS web-build
WORKDIR /app
COPY web/package.json web/package-lock.json ./web/
RUN npm ci --prefix web --registry=https://registry.npmmirror.com
COPY web ./web
RUN npm run build --prefix web
FROM ${NGINX_IMAGE}
COPY public /srv/public
COPY --from=web-build /app/public/community /srv/public/community
COPY deploy/community/nginx.conf /etc/nginx/nginx.conf
