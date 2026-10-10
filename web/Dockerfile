FROM node:22-alpine AS builder
WORKDIR /app

# The API base URL is inlined into the bundle at build time, so it must be a build argument.
# The default of "same-origin" keeps requests relative, which is what a container deployment wants:
# nginx serves this web app and proxies /api/ to the API. A deployment that talks to an API on
# another host overrides it with that URL. Leaving it unset would fall back to the development default
# (http://localhost:8080) in the browser, so it is never left unset on purpose.
ARG VITE_API_URL="same-origin"
ENV VITE_API_URL=${VITE_API_URL}

COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# The bundle is static files, so the runtime image is nginx rather than Node: nothing at runtime needs a
# JavaScript engine, and a web server serving files is smaller, starts instantly and has no process to
# supervise. Production does the same thing with the host's own nginx (see deploy/nginx/nginx-prod.conf).
FROM nginx:alpine
COPY nginx-spa.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
