FROM --platform=$BUILDPLATFORM node:22-alpine AS panel
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
COPY web/vendor/picomatch/ ./vendor/picomatch/
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb AS backend
WORKDIR /build
COPY go.mod go.sum ./
COPY torznab/ ./torznab/
RUN go mod download
COPY internal/ ./internal/
COPY cmd/ ./cmd/
COPY web/assets_embed.go ./web/assets_embed.go
COPY --from=panel /build/web/dist/ ./web/dist/
ARG VERSION=2.5.0
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -tags production -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /aiosource ./cmd/aiosources \
    && mkdir -p /runtime/data/providers /runtime/tmp \
    && chmod 700 /runtime/data /runtime/data/providers \
    && chmod 1777 /runtime/tmp

FROM alpine:3.23 AS pgtools
RUN apk add --no-cache postgresql18-client \
    && mkdir -p /pgtools-root/usr/bin /pgtools-root/lib /pgtools-root/usr/lib \
    && cp -L "$(command -v pg_dump)" /pgtools-root/usr/bin/pg_dump \
    && cp -L "$(command -v pg_restore)" /pgtools-root/usr/bin/pg_restore \
    && for bin in /pgtools-root/usr/bin/pg_dump /pgtools-root/usr/bin/pg_restore; do \
         ldd "$bin" | awk '{for(i=1;i<=NF;i++) if ($i ~ /^\//) print $i}' | sort -u | while read -r lib; do \
           mkdir -p "/pgtools-root$(dirname "$lib")" && cp -L "$lib" "/pgtools-root$lib"; \
         done; \
       done \
    && for ld in /lib/ld-musl-*.so.1; do cp -L "$ld" "/pgtools-root$ld"; done \
    && chroot /pgtools-root /usr/bin/pg_dump --version \
    && chroot /pgtools-root /usr/bin/pg_restore --version

FROM scratch
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=pgtools /pgtools-root/ /
COPY --from=backend /aiosource /aiosource
COPY --from=backend --chown=65532:65532 /runtime/ /
COPY LICENSE /usr/share/licenses/aiosource/LICENSE
COPY web/public/THIRD-PARTY-NOTICES.txt /usr/share/licenses/aiosource/THIRD-PARTY-NOTICES.txt
ENV DATA_DIR=/data \
    PROVIDERS_DIR=/data/providers \
    BIND=0.0.0.0:7000 \
    PATH=/usr/bin:/bin \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
WORKDIR /data
USER 65532:65532
EXPOSE 7000
HEALTHCHECK --interval=30s --timeout=15s --start-period=30s CMD ["/aiosource", "healthcheck"]
ENTRYPOINT ["/aiosource"]
CMD ["serve"]
