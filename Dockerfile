FROM golang:1.23 AS builder

WORKDIR /app

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/build/linux/gui-sync main.go

RUN CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /app/build/windows/gui-sync.exe main.go

FROM alpine:latest AS final

# Copy the binaries from the builder stage to the final image
COPY --from=builder /app/build /build