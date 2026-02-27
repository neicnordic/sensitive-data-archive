FROM "golang:${GOLANG_VERSION:-1.24}-alpine" AS Build
ENV GO111MODULE=on
ENV GOPATH=$PWD
ENV CGO_ENABLED=0
ENV GOOS=linux

COPY . .
SHELL ["sh", "-c"]
RUN set -ex; for p in cmd/*; do go build -buildvcs=false -o "${p/cmd\//sda-}" "./$p"; done

FROM gcr.io/distroless/static-debian11

ARG SOURCE_COMMIT

COPY --from=Build /go/sda-* /usr/local/bin/
COPY --from=Build /go/cmd/auth/frontend /frontend
COPY --from=Build /go/schemas /schemas

USER 65534
