FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends python3 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download all
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
ARG AGENT_VERSION=1.5.13
ARG VERSION_HASH=""
RUN set -eu; \
    if [ -z "$VERSION_HASH" ]; then VERSION_HASH="$(git rev-parse HEAD)"; fi; \
    make artifacts DIST=/out GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
        AGENT_VERSION="$AGENT_VERSION" VERSION_HASH="$VERSION_HASH"

# Export binaries and legal documents without running a container.
FROM scratch AS artifacts
COPY --from=build /out/ /

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 shellbeam \
    && useradd --uid 10001 --gid shellbeam --no-create-home shellbeam \
    && mkdir -p /app \
    && chown shellbeam:shellbeam /app \
    && touch /.komari-agent-container
WORKDIR /app
COPY --from=build /out/shellbeam-monitoring-agent /usr/local/bin/shellbeam-monitoring-agent
COPY --from=build /out/LICENSE /out/NOTICE /out/UPSTREAM.md /usr/share/doc/shellbeam-monitoring-agent/
COPY --from=build /out/THIRD_PARTY_NOTICES /usr/share/doc/shellbeam-monitoring-agent/THIRD_PARTY_NOTICES
ARG AGENT_VERSION=1.5.13
LABEL org.opencontainers.image.title="ShellBeam Monitoring Agent" \
      org.opencontainers.image.description="Independent Komari-compatible monitoring agent for ShellBeam Server" \
      org.opencontainers.image.source="https://github.com/0xJean/shellbeam-monitoring-agent" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="$AGENT_VERSION"
# JSON config has higher priority than environment variables; keep
# disable_auto_update=true there as well. Replace images to update containers.
ENV AGENT_DISABLE_AUTO_UPDATE=true
USER shellbeam:shellbeam
ENTRYPOINT ["/usr/local/bin/shellbeam-monitoring-agent"]
CMD ["--help"]
