# Build stage
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git make gcc musl-dev linux-headers

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binaries with version info
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -X github.com/ram1234598766-dotcom/Local-WEB/internal/version.Version=${VERSION} -X github.com/ram1234598766-dotcom/Local-WEB/internal/version.Commit=${COMMIT} -X github.com/ram1234598766-dotcom/Local-WEB/internal/version.Date=${DATE}" -o /localweb ./cmd/node
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /localweb-cli ./cmd/cli

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    iproute2 \
    iptables \
    wireguard-tools \
    dbus \
    avahi \
    bluez \
    libcap \
    shadow

# Create localweb user
RUN addgroup -S localweb && \
    adduser -S -D -H -h /var/lib/localweb -s /sbin/nologin -G localweb localweb

# Create directories
RUN mkdir -p /etc/localweb /var/lib/localweb /var/log/localweb /var/run/localweb && \
    chown -R localweb:localweb /var/lib/localweb /var/log/localweb /var/run/localweb

# Copy binaries
COPY --from=builder /localweb /usr/bin/localweb
COPY --from=builder /localweb-cli /usr/bin/localweb-cli

# Copy config
COPY config/config.json /etc/localweb/config.json
RUN chown root:localweb /etc/localweb/config.json && chmod 640 /etc/localweb/config.json

# Set capabilities for VPN (TUN device access)
RUN setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw,cap_sys_admin,cap_dac_override,cap_dac_read_search,cap_sys_resource,cap_sys_nice+ep' /usr/bin/localweb || true

# Expose ports
EXPOSE 4443/udp 5353/udp 8080/tcp

# Volumes
VOLUME ["/var/lib/localweb", "/etc/localweb", "/var/log/localweb"]

# Environment
ENV LOCALWEB_DATA_DIR=/var/lib/localweb
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

# Run as non-root user
USER localweb

# Entry point
ENTRYPOINT ["/usr/bin/localweb"]
CMD ["node", "--data-dir", "/var/lib/localweb"]