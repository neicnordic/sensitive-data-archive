FROM golang:1.26-alpine AS builder

ENV GOPATH=$PWD
ENV CGO_ENABLED=0

COPY . .

RUN go build -buildvcs=false -o ./sda-download ./cmd
RUN echo "nobody:x:65534:65534:nobody:/:/sbin/nologin" > passwd

FROM scratch

ARG BUILD_DATE
ARG SOURCE_COMMIT

COPY --from=builder /go/passwd /etc/passwd
COPY --from=builder /go/sda-* /usr/bin/
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

USER 65534
