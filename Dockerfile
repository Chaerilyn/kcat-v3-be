# --- Build Stage ---
    FROM golang:1.25-alpine AS builder

    WORKDIR /app
    
    # Copy dependency files first (for better caching)
    COPY go.mod go.sum ./
    RUN go mod download
    
    # Copy source code
    COPY . .
    
    # Build the binary (named "myapp")
    RUN CGO_ENABLED=0 go build -o myapp main.go
    
    # --- Run Stage ---
    FROM alpine:latest
    
    WORKDIR /app
    
    # Install certificates so your Bot can talk to Discord HTTPS API
    RUN apk add --no-cache ca-certificates
    
    # Copy the binary from the builder
    COPY --from=builder /app/myapp /app/myapp
    
    # Create the data directory
    RUN mkdir /app/pb_data
    
    # Expose port 8080 (PocketBase default)
    EXPOSE 8080
    
    # Run the app
    # 0.0.0.0 is required for Railway to map the port correctly
    CMD ["/app/myapp", "serve", "--http=0.0.0.0:8080"]