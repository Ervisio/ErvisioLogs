# syntax=docker/dockerfile:1
# Build context: repository root.

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /src/web/dist/ ./internal/webui/dist/
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/Ervisio/ErvisioLogs/backend/internal/version.Version=${VERSION} -X github.com/Ervisio/ErvisioLogs/backend/internal/version.Commit=${COMMIT}" \
      -o /out/ervisio-logs ./cmd/ervisio-logs

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ervisio-logs /usr/local/bin/ervisio-logs
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/ervisio-logs", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/ervisio-logs"]
CMD ["serve"]
