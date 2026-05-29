FROM golang:1.23-alpine AS build
WORKDIR /src
COPY common ./common
COPY payment ./payment
WORKDIR /src/payment
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/payment ./cmd/payment

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/payment /app/payment
EXPOSE 8060
ENTRYPOINT ["/app/payment"]
