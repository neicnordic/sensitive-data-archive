FROM golang:1.25-alpine AS apptainer_builder

RUN apk add --no-cache \
        # Required for apptainer to find min go version
        bash \
        binutils-gold \
        cryptsetup \
        gawk \
        gcc \
        git \
        libc-dev \
        linux-headers \
        libressl-dev \
        libuuid \
        libseccomp-dev \
        make \
        util-linux-dev

ARG APPTAINER_COMMITISH="v1.4.3"
ARG MCONFIG_OPTIONS="--with-suid"
WORKDIR $GOPATH/src/github.com/apptainer
RUN git clone https://github.com/apptainer/apptainer.git \
    && cd apptainer \
    && git checkout "$APPTAINER_COMMITISH" \
    && ./mconfig $MCONFIG_OPTIONS -p /usr/local/apptainer \
    && cd builddir \
    && make \
    && make install


FROM golang:1.25-alpine AS app_builder

ENV CGO_ENABLED=0

COPY . .

RUN go build -buildvcs=false -o ./sda-validator-orchestrator

FROM golang:1.25-alpine

ARG BUILD_DATE
ARG SOURCE_COMMIT

LABEL maintainer="NeIC System Developers"
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=$BUILD_DATE
LABEL org.label-schema.vcs-url="https://github.com/neicnordic/sensitive-data-archive"
LABEL org.label-schema.vcs-ref=$SOURCE_COMMIT

COPY --from=app_builder /go/sda-validator-orchestrator /usr/bin/

COPY --from=apptainer_builder /usr/local/apptainer /usr/local/apptainer
COPY --from=app_builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENV PATH="/usr/local/apptainer/bin:$PATH" \
    APPTAINER_TMPDIR="/tmp-apptainer"
RUN apk add --no-cache ca-certificates libseccomp squashfs-tools tzdata \
    && mkdir -p $APPTAINER_TMPDIR \
    && cp /usr/share/zoneinfo/UTC /etc/localtime \
    && apk del tzdata \
    && rm -rf /tmp/* /var/cache/apk/*

CMD ["sda-validator-orchestrator"]