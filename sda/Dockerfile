FROM golang:1.25.7-trixie AS Build
ENV CGO_ENABLED=0

COPY . .
SHELL ["bash", "-c"]
RUN set -ex; for p in cmd/*; do go build -buildvcs=false -o "${p/cmd\//sda-}" "./$p"; done

FROM gcr.io/distroless/static-debian13

COPY --from=Build /go/sda-* /usr/local/bin/
COPY --from=Build /go/cmd/auth/frontend /frontend
COPY --from=Build /go/schemas /schemas

USER 65534
