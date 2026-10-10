# syntax=docker/dockerfile:1
# One small image: the React UI is compiled and embedded into a static Go
# binary, which runs on a distroless base (~20 MB total).

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/roarr ./cmd/roarr \
 && mkdir -p /out/config

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="roarr" \
      org.opencontainers.image.description="One fast UI for Sonarr, Radarr, Prowlarr, Seerr and Jellyfin" \
      org.opencontainers.image.source="https://github.com/noxaur/roarr" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/roarr /roarr
COPY --from=build --chown=65532:65532 /out/config /config
ENV ROARR_DATA=/config PORT=3847
VOLUME /config
EXPOSE 3847
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/roarr", "-healthcheck"]
ENTRYPOINT ["/roarr"]
