# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /shop-alarm .

# Distroless static: CA certificates included (ntfy/Frigate over TLS).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /shop-alarm /shop-alarm
EXPOSE 9101
ENTRYPOINT ["/shop-alarm"]