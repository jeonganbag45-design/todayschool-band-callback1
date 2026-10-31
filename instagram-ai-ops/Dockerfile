FROM golang:1.23-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -tags netgo -ldflags="-s -w" -o /out/app .

FROM alpine:3.20
RUN adduser -D -u 10001 appuser
WORKDIR /app
COPY --from=build /out/app /app/app
USER appuser
EXPOSE 10000
CMD ["/app/app"]
