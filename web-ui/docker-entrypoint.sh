#!/bin/sh

# Set default backend API address
API_BACKEND_URL=${API_BACKEND_URL:-http://waverless-svc:80}

echo "Configuring Nginx with backend: ${API_BACKEND_URL}"

# Use envsubst to replace environment variables
envsubst '${API_BACKEND_URL}' < /etc/nginx/conf.d/default.conf.template > /etc/nginx/conf.d/default.conf

# Validate configuration
nginx -t

# Start Nginx
exec nginx -g 'daemon off;'
