# Build context must be repository root:
#   docker build -f server/Dockerfile .

FROM node:24-alpine AS web-builder
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.27-alpine AS server-builder
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web-builder /src/web/dist/ ./internal/webui/dist/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/tianshu ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=server-builder /out/tianshu /app/tianshu
COPY --from=server-builder /src/server/configs/config.example.toml /app/configs/config.example.toml
EXPOSE 8080
ENTRYPOINT ["/app/tianshu"]
