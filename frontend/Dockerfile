# Stage 1: Build static assets
FROM node:24-alpine AS builder

WORKDIR /app

COPY package*.json ./
RUN npm install

COPY . .

# Accept the bypass flag as a build-time argument.
# Vite bakes VITE_* vars into the bundle at build time, so this must be
# set before `npm run build` runs — it cannot be changed at container runtime.
ARG VITE_DEV_BYPASS_ENABLED=false
ENV VITE_DEV_BYPASS_ENABLED=$VITE_DEV_BYPASS_ENABLED

RUN npm run build

# Stage 2: Serve using Nginx
FROM nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
