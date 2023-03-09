FROM postgres:13.8-alpine3.16

ARG BUILD_DATE
ARG SOURCE_COMMIT

LABEL maintainer "NeIC System Developers"
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=$BUILD_DATE
LABEL org.label-schema.vcs-url="https://github.com/neicnordic/sda-db"
LABEL org.label-schema.vcs-ref=$SOURCE_COMMIT

ENV SSL_SUBJ /C=SE/ST=Sweden/L=Uppsala/O=NBIS/OU=SysDevs/CN=LocalEGA
ENV TZ       Europe/Stockholm

RUN apk -U upgrade && \
    apk add openssl && \
    rm -rf /var/cache/apk/*

COPY initdb.d      /docker-entrypoint-initdb.d
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
COPY migratedb.d   /migratedb.d

RUN chmod 755 /usr/local/bin/entrypoint.sh

USER 70

HEALTHCHECK --interval=3s \
    CMD pg_isready -U lega_out -h localhost

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
