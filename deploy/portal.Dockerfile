FROM node:26.13@sha256:e18c3ccdf7d71619990b0e85639f506e2028a95410999cab24cc373cbd10e0ac AS build

WORKDIR /app

# Copy package files first for better caching
COPY portal/package.json portal/package-lock.json* ./

# Install dependencies
RUN npm ci

# Copy source code
COPY portal/ .

# Build the Angular application for production
RUN npm run build

FROM nginx:alpine@sha256:fb4658584bd9a916ffb6f6e365f97c25cfdb2923801f551ea08356e695f14ff7
# Angular 17+ outputs to dist/<project>/browser
RUN rm -rf /usr/share/nginx/html/*
COPY --from=build /app/dist/kbind-provider-portal/browser /usr/share/nginx/html
COPY deploy/nginx.conf /etc/nginx/nginx.conf

# Fix permissions for non-root nginx user (uid 101)
RUN mkdir -p /var/cache/nginx /var/run /var/log/nginx && \
    chown -R 101:101 /var/cache/nginx /var/run /var/log/nginx /etc/nginx/conf.d

EXPOSE 8080
USER 101
