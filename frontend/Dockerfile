# Stage 1: build the static bundle
FROM node:20-alpine AS builder
WORKDIR /app

COPY package*.json ./
RUN npm install
COPY . .

# Vite inlines env vars at BUILD time, not at runtime: this value ends up
# hardcoded in the JS bundle, so it must be passed as a build arg.
ARG VITE_API_URL=http://localhost:8000
ENV VITE_API_URL=$VITE_API_URL
RUN npm run build

# Stage 2: serve the static files
FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
