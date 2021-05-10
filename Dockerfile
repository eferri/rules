FROM golang:1.16.5-buster as dev

WORKDIR /app

# Cache dependencies
COPY ./go.mod ./go.sum ./
RUN go mod download
COPY . .

# Install delve debugger
RUN go install github.com/go-delve/delve/cmd/dlv@v1.6.1

# Download and build dependencies
RUN go build -o battlesnake ./cli/battlesnake/main.go

CMD ["./battlesnake", "play", "-b"]
