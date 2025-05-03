# Stage 1: Build the application
FROM golang AS builder

# Set the working directory
WORKDIR /app

# Copy the Go module files first to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the application with static linking for distroless compatibility
RUN CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o portscan .

# Stage 2: Create the final minimal image
FROM gcr.io/distroless/static:nonroot

# Copy the binary from the builder stage
COPY --from=builder --chmod=755 /app/portscan /portscan

# Use the nonroot user provided by the distroless image
USER nonroot:nonroot

# Document which port is used by the application
EXPOSE 8080

# Run the binary
ENTRYPOINT ["/portscan"]