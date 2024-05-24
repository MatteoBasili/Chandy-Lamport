# Use an official Go image as a base
FROM golang:1.22

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code except the files specified in .dockerignore
COPY . .

# Build the application
RUN go build -o app ./app.go

# Command to run the application
CMD ["./app"]