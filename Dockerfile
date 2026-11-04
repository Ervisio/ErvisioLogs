# syntax=docker/dockerfile:1
# Build context: repository root.
# Base images are pinned by multi-arch index digest (verified 2026-11-02). Bump deliberately.

FROM --platform=$BUILDPLATFORM node:24-alpine@sha256:8a32c8cf649a49915a66e81e7a609223c708d0adabf288ee3f4077d765542f85 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /src/web/dist/ ./internal/webui/dist/
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
# Cross-compiled on the build platform: a multi-arch build needs no emulation (the final stage has no RUN).
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/Ervisio/ErvisioLogs/backend/internal/version.Version=${VERSION} -X github.com/Ervisio/ErvisioLogs/backend/internal/version.Commit=${COMMIT}" \
      -o /out/ervisio-logs ./cmd/ervisio-logs
RUN mkdir -p /out/data/spool /out/data/cache /out/data/tls /out/data/socket

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/ervisio-logs /usr/local/bin/ervisio-logs
# 65532 is "nonroot" in distroless. Without this, Docker would create the volume mount points as root.
COPY --from=build --chown=65532:65532 /out/data /var/lib/ervisio-logs
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/ervisio-logs", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/ervisio-logs"]
CMD ["serve"]
