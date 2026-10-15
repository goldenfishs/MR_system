FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run typecheck && npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mr-system .

FROM alpine:3.23
RUN addgroup -S -g 101 app && adduser -S -u 100 app -G app \
    && apk add --no-cache ca-certificates tzdata python3 py3-reportlab py3-pillow poppler-utils libreoffice-writer font-wqy-zenhei \
    && mkdir -p /app/data && chown -R app:app /app
WORKDIR /app
COPY --from=backend /out/mr-system /app/mr-system
COPY --from=frontend /src/frontend/dist /app/web
USER app
ENV ADDR=0.0.0.0:8080 DATA_DIR=/app/data WEB_DIR=/app/web DEMO_MODE=false SESSION_SECURE=true
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["/app/mr-system"]
