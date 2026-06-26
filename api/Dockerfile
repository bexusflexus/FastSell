FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/fastsell-api ./cmd/fastsell-api

FROM alpine:3.20

WORKDIR /app
COPY --from=build /out/fastsell-api /app/fastsell-api

EXPOSE 8888

ENTRYPOINT ["/app/fastsell-api"]
