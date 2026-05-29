FROM golang:1.23-alpine AS build
WORKDIR /src
COPY common ./common
COPY gateway ./gateway
WORKDIR /src/gateway
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/gateway ./cmd/gateway

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/gateway /app/gateway
EXPOSE 8080
ENTRYPOINT ["/app/gateway"]
