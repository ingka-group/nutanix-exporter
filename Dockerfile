# syntax=docker/dockerfile:1

# Build the application from source
FROM golang:1.21 AS build-stage

# Set the working directory inside the container
WORKDIR /app

# Fetch dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source from the current directory to the Working Directory inside the container
COPY . .

# Build the Go app
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags netgo -ldflags '-w' -o nutanix-exporter ./cmd/nutanix-exporter

# Deploy the application binary into a lean image
FROM gcr.io/distroless/base-debian11 AS build-release-stage

WORKDIR /

# Copy the binary from build-stage
COPY --from=build-stage /app/nutanix-exporter /

# Copy configs
COPY --from=build-stage /app/configs /configs

LABEL description "Prometheus Exporter for Nutanix Prism Element"

USER nonroot:nonroot

EXPOSE 9408
ENTRYPOINT ["/nutanix-exporter"]
