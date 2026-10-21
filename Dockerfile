FROM --platform=$BUILDPLATFORM node:22-alpine AS panel
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.10-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS backend
WORKDIR /build
COPY go.mod go.sum ./
COPY torznab/ ./torznab/
RUN go mod download
COPY internal/ ./internal/
COPY cmd/ ./cmd/
COPY web/assets_embed.go ./web/assets_embed.go
COPY --from=panel /build/web/dist/ ./web/dist/
ARG VERSION=2.0.0
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -tags production -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /aiosource ./cmd/ingest \
    && mkdir -p /runtime/data/providers /runtime/tmp \
    && chmod 700 /runtime/data /runtime/data/providers \
    && chmod 1777 /runtime/tmp

FROM scratch
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=backend /aiosource /aiosource
COPY --from=backend --chown=65532:65532 /runtime/ /
COPY LICENSE /usr/share/licenses/aiosource/LICENSE
COPY web/public/THIRD-PARTY-NOTICES.txt /usr/share/licenses/aiosource/THIRD-PARTY-NOTICES.txt
ENV DATA_DIR=/data \
    PROVIDERS_DIR=/data/providers \
    BIND=0.0.0.0:7000
WORKDIR /data
USER 65532:65532
EXPOSE 7000
HEALTHCHECK --interval=30s --timeout=15s --start-period=30s CMD ["/aiosource", "healthcheck"]
ENTRYPOINT ["/aiosource"]
CMD ["serve"]
