FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" -o /out/mainspring ./cmd/mainspring \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="Mainspring" \
      org.opencontainers.image.description="One endpoint for all your AI subscriptions: pools Claude, Codex, Grok, Cursor and OpenAI-compatible accounts, keeps sessions on one account and routes by quota." \
      org.opencontainers.image.source="https://github.com/kacperkwapisz/mainspring" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/mainspring /mainspring
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot:nonroot
ENV HOST=0.0.0.0 \
    MAINSPRING_INSTALL=docker \
    PORT=8317 \
    DATABASE_PATH=/data/mainspring.sqlite
EXPOSE 8317
VOLUME /data
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD ["/mainspring", "-healthcheck"]
ENTRYPOINT ["/mainspring"]
