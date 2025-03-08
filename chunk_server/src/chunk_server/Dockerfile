
FROM golang:1.23-alpine
WORKDIR /app

# Copy the Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy all files
COPY . .

# Build and run the server
RUN go build -o gin-server
EXPOSE 8080
CMD ["/app/gin-server"]
