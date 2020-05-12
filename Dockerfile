FROM golang:1-alpine AS build
COPY . /src
RUN apk --no-cache add git && cd /src && go install .

FROM alpine:latest
WORKDIR /app
COPY --from=build /go/bin/thanos-remote-read .

ENTRYPOINT ["/app/thanos-remote-read"]
EXPOSE 10080
