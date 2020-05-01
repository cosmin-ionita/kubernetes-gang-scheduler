# build stage
FROM golang:1.11-alpine as backend
RUN apk add --update --no-cache bash ca-certificates curl git make tzdata

RUN mkdir -p /go/src/gang-scheduler
ADD Gopkg.* Makefile /go/src/gang-scheduler/
WORKDIR /go/src/gang-scheduler
RUN make vendor
ADD . /go/src/gang-scheduler

RUN make build

FROM alpine:3.7
COPY --from=backend /usr/share/zoneinfo/ /usr/share/zoneinfo/
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=backend /go/src/gang-scheduler/build/scheduler /bin

ENTRYPOINT ["/bin/scheduler"]

