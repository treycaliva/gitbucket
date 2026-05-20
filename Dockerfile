# Stage 1: Build Frontend
FROM node:20-slim AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build Go Backend
FROM golang:1.25 AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gitbucket main.go

# Stage 3: Runtime
FROM debian:bookworm-slim
WORKDIR /app

# Install git and ca-certificates (required for git-http-backend CGI and HTTPS API requests)
RUN apt-get update && apt-get install -y git ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy built backend binary
COPY --from=backend-builder /app/gitbucket ./gitbucket

# Copy built frontend assets
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

# Set default env variables
ENV PORT=8080

# Expose port
EXPOSE 8080

# Start server
CMD ["./gitbucket"]

