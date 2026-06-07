FROM golang:1.25.5-trixie AS build

ARG USER=superphenix-telemetry
ARG UID=1000

RUN adduser               \
  --disabled-password     \
  --gecos ""              \
  --home "/nonexistent"   \
  --shell "/sbin/nologin" \
  --no-create-home        \
  --uid $UID              \
  $USER

WORKDIR /src
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -a -installsuffix cgo \
    -ldflags '-w -s' \
    -o /bin/superphenix-telemetry \
    ./cmd/server

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group

WORKDIR /app
COPY --from=build /bin/superphenix-telemetry /app/superphenix-telemetry

USER superphenix-telemetry:superphenix-telemetry

EXPOSE 8080

ENTRYPOINT ["/app/superphenix-telemetry"]
