FROM golang:1.22-alpine AS gobuilder
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/emby-ecer .

FROM node:20-alpine AS webbuilder
WORKDIR /app
COPY frontend/package.json ./
RUN npm install --legacy-peer-deps 2>&1 || npm install
COPY frontend/ ./
RUN npm run build

FROM alpine:3.20
ENV PORT=3000 \
    CONFIG_PATH=/data/config.json \
    PUBLIC_DIR=/app/public \
    PANSOU_URL=https://so.252035.xyz
WORKDIR /app
COPY --from=gobuilder /out/emby-ecer /app/emby-ecer
COPY --from=webbuilder /app/dist ./public
RUN mkdir -p /data
VOLUME ["/data"]
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD sh -c "wget -qO- http://127.0.0.1:${PORT:-3000}/api/health >/dev/null || exit 1"
CMD ["/app/emby-ecer"]
