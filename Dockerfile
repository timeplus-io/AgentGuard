# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build backend with embedded frontend assets
FROM golang:1.23-alpine AS backend-builder
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Embed the frontend dist into the binary
COPY --from=frontend-builder /app/frontend/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -tags embed -o server cmd/server/main.go

# Stage 3: Final image — single binary, no assets directory needed
FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache ca-certificates \
 && addgroup -S agentguard \
 && adduser -S agentguard -G agentguard \
 && mkdir -p /app/catalog \
 && chown -R agentguard:agentguard /app
COPY --chown=agentguard:agentguard --from=backend-builder /app/backend/server ./server
USER agentguard
EXPOSE 8080
CMD ["./server"]
