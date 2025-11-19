FROM debian:trixie-slim AS apptainer_builder

RUN apt update \
      && apt install -y wget \
      && cd /tmp \
      && wget https://github.com/apptainer/apptainer/releases/download/v1.4.3/apptainer_1.4.3-trixie+_amd64.deb \
      && apt install -y ./apptainer_1.4.3-trixie+_amd64.deb libsubid5 squashfs-tools

COPY ./apptainer.conf /etc/apptainer/apptainer.conf

FROM golang:1.25-alpine AS app_builder

ENV CGO_ENABLED=0

COPY . .

RUN go build -buildvcs=false -o ./sda-validator-orchestrator


FROM gcr.io/distroless/static-debian12

ARG BUILD_DATE
ARG SOURCE_COMMIT

LABEL maintainer="NeIC System Developers"
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=$BUILD_DATE
LABEL org.label-schema.vcs-url="https://github.com/neicnordic/sensitive-data-archive"
LABEL org.label-schema.vcs-ref=$SOURCE_COMMIT


COPY --from=app_builder /go/sda-validator-orchestrator /usr/bin/
COPY --from=app_builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=apptainer_builder /bin/apptainer /bin/apptainer
COPY --from=apptainer_builder /etc/apptainer/ /etc/apptainer/
COPY --from=apptainer_builder /var/lib/apptainer /var/lib/apptainer

# Copy libraries needed by apptainer and any executables that apptainer needs
COPY --from=apptainer_builder /lib64/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2
COPY --from=apptainer_builder /lib/x86_64-linux-gnu /lib/x86_64-linux-gnu
COPY --from=apptainer_builder /usr/libexec/apptainer/bin/starter /libexec/apptainer/bin/starter

COPY --from=apptainer_builder /bin/unsquashfs /bin/unsquashfs
COPY --from=apptainer_builder /etc/localtime /etc/localtime


ENV PATH="/usr/local/apptainer/:$PATH"

USER 65534
CMD ["sda-validator-orchestrator"]