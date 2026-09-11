# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/wallet ./cmd/server

# ---- runtime stage ----
# distroless static: ~2MB base, no shell, runs as non-root (uid 65532).
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /bin/wallet /wallet

EXPOSE 8080

# No shell/curl in distroless, so the binary probes itself.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["/wallet", "--healthcheck"]

ENTRYPOINT ["/wallet"]
