# Stage 1: Build React App
FROM --platform=linux/amd64 node:20-alpine AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./
COPY pnpm-lock.yaml* ./

# Install pnpm and dependencies
RUN npm install -g pnpm && pnpm install --frozen-lockfile

# Copy source code
COPY . .

# Build args for credentials (will be used at build time)
ARG VITE_ADMIN_USERNAME=admin
ARG VITE_ADMIN_PASSWORD=admin

# Set environment variables for build
ENV VITE_ADMIN_USERNAME=$VITE_ADMIN_USERNAME
ENV VITE_ADMIN_PASSWORD=$VITE_ADMIN_PASSWORD

# Build the app
RUN pnpm run build

# Stage 2: Serve with Nginx
FROM --platform=linux/amd64 nginx:alpine

# Install gettext for envsubst
RUN apk add --no-cache gettext

# Copy built files to nginx
COPY --from=builder /app/dist /usr/share/nginx/html

# Copy nginx configuration template and entrypoint script
COPY nginx.conf.template /etc/nginx/conf.d/default.conf.template
COPY docker-entrypoint.sh /docker-entrypoint.sh

# Make entrypoint executable
RUN chmod +x /docker-entrypoint.sh

# Set default environment variable
ENV API_BACKEND_URL=http://waverless-svc:80

# Expose port
EXPOSE 80

# Use custom entrypoint
ENTRYPOINT ["/docker-entrypoint.sh"]
