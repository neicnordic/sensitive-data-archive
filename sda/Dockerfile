FROM "golang:${GOLANG_VERSION:-1.23}-bullseye" AS Build
ENV GO111MODULE=on
ENV GOPATH=$PWD
ENV CGO_ENABLED=0
ENV GOOS=linux

COPY . .
SHELL ["bash", "-c"]
RUN set -ex; for p in cmd/*; do go build -buildvcs=false -o "${p/cmd\//sda-}" "./$p"; done

FROM debian:bullseye-slim AS Debug

ARG SOURCE_COMMIT

LABEL org.opencontainers.image.authors="NeIC System Developers"
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=now()
LABEL org.label-schema.vcs-url="https://github.com/neicnordic/sensitive-data-archive"
LABEL org.label-schema.vcs-ref=$SOURCE_COMMIT

COPY --from=Build /go/sda-* /usr/local/bin/
COPY --from=Build /go/cmd/auth/frontend /frontend
COPY --from=Build /go/schemas /schemas

USER 65534

FROM gcr.io/distroless/static-debian11

ARG SOURCE_COMMIT

LABEL org.opencontainers.image.authors="NeIC System Developers"
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=now()
LABEL org.label-schema.vcs-url="https://github.com/neicnordic/sensitive-data-archive"
LABEL org.label-schema.vcs-ref=$SOURCE_COMMIT

COPY --from=Build /go/sda-* /usr/local/bin/
COPY --from=Build /go/cmd/auth/frontend /frontend
COPY --from=Build /go/schemas /schemas

USER 65534
