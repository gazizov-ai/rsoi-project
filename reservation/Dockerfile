FROM golang:1.23-alpine AS build
WORKDIR /src
COPY common ./common
COPY reservation ./reservation
WORKDIR /src/reservation
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/reservation ./cmd/reservation

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/reservation /app/reservation
EXPOSE 8070
ENTRYPOINT ["/app/reservation"]
