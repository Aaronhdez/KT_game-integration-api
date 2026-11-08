# Build stage: compiles the API into a single static binary.
FROM golang:1.27.3-alpine AS build

WORKDIR /src

# Dependencies first: this layer is cached until go.mod or go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 gives a statically linked binary that needs no C libraries.
# -trimpath removes local paths; -s -w drop debug symbols (smaller binary).
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./Startup

# Runtime stage: only the binary. Migrations are embedded in it.
# Alpine (not scratch/distroless) so the Compose healthcheck can use wget.
FROM alpine:3.22

RUN adduser -D -u 10001 app
COPY --from=build /out/api /app/api

USER app
EXPOSE 8080

# Exec form: the binary is PID 1 and receives SIGTERM, so it shuts down gracefully.
ENTRYPOINT ["/app/api"]
