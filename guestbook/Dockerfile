# Build stage: Go toolchain + cached dependencies, statically linked binary so it can run on scratch
FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o app .

# Production stage: minimal image
FROM scratch
WORKDIR /app
COPY --from=builder /app/app .
COPY public/ ./public/
EXPOSE 3000
ENTRYPOINT ["./app"]
