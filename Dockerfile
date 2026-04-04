# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build backend
FROM golang:1.21-alpine AS backend-builder
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o server cmd/server/main.go

# Stage 3: Final image
FROM alpine:3.19
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=backend-builder /app/backend/server ./server
COPY --from=frontend-builder /app/frontend/dist ./public
EXPOSE 8080
CMD ["./server"]
