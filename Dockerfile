FROM golang:1-alpine
COPY . /src
RUN apk --no-cache add git && cd /src && go install .

FROM alpine:latest
WORKDIR /app
COPY --from=0 /go/bin/thanos-remote-read .

ENTRYPOINT ["/app/thanos-remote-read"]
EXPOSE 10080
