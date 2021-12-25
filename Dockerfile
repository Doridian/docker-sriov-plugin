FROM golang:1.17.5 as build
WORKDIR /usr/local/go/src/github.com/doridian/docker-sriov-plugin

COPY . .
RUN go install -v

FROM debian:stable-slim
COPY --from=build /go/bin/docker-sriov-plugin /bin/docker-sriov-plugin
COPY ibdev2netdev /usr/local/bin

CMD ["/bin/docker-sriov-plugin"]
