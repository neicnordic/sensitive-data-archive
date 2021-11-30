FROM rabbitmq:3.8.16-management-alpine

ARG BUILD_DATE
ARG SOURCE_COMMIT

LABEL maintainer "EGA System Developers"
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=$BUILD_DATE
LABEL org.label-schema.vcs-url="https://github.com/neicnordic/LocalEGA-mq"
LABEL org.label-schema.vcs-ref=$SOURCE_COMMIT

ENV RABBITMQ_CONFIG_FILE=/var/lib/rabbitmq/rabbitmq
ENV RABBITMQ_ADVANCED_CONFIG_FILE=/var/lib/rabbitmq/advanced
ENV RABBITMQ_LOG_BASE=/var/lib/rabbitmq

RUN apk add --no-cache ca-certificates openssl

RUN rabbitmq-plugins enable --offline rabbitmq_federation rabbitmq_federation_management rabbitmq_shovel rabbitmq_shovel_management

COPY entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/docker-entrypoint.sh

USER 100:101

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

CMD ["rabbitmq-server"]
