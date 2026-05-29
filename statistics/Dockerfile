FROM golang:1.23-alpine AS build
WORKDIR /src
COPY common ./common
COPY statistics ./statistics
WORKDIR /src/statistics
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/statistics ./cmd/statistics

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/statistics /app/statistics
EXPOSE 8040
ENTRYPOINT ["/app/statistics"]
