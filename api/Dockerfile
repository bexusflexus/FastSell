FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/fastsell-api ./cmd/fastsell-api

FROM alpine:3.20

ARG FASTSELL_UID=956
ARG FASTSELL_GID=956

RUN addgroup -g "${FASTSELL_GID}" -S fastsell && \
    adduser -u "${FASTSELL_UID}" -S fastsell -G fastsell

WORKDIR /app
COPY --from=build /out/fastsell-api /app/fastsell-api

USER fastsell
EXPOSE 8888

ENTRYPOINT ["/app/fastsell-api"]
