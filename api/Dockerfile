FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/fastsell-api ./cmd/fastsell-api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/fastsell-backup ./cmd/fastsell-backup

FROM alpine:3.20

RUN apk add --no-cache postgresql16-client tar tzdata zstd

WORKDIR /app
COPY --from=build /out/fastsell-api /app/fastsell-api
COPY --from=build /out/fastsell-backup /app/fastsell-backup

EXPOSE 8888

ENTRYPOINT ["/app/fastsell-api"]
