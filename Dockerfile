FROM golang:1.17.5 as build
WORKDIR /go/src/github.com/doridian/docker-sriov-plugin

COPY . .
RUN go install -v docker-sriov-plugin

FROM debian:stretch-slim
COPY --from=build /go/bin/docker-sriov-plugin /bin/docker-sriov-plugin
COPY ibdev2netdev /usr/local/bin

CMD ["/bin/docker-sriov-plugin"]
