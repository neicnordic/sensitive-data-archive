FROM rabbitmq:3.13.7-management-alpine

ARG BUILD_DATE
ARG SOURCE_COMMIT

LABEL org.opencontainers.image.authors="NeIC System Developers"
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=$BUILD_DATE
LABEL org.label-schema.vcs-url="https://github.com/neicnordic/sda"
LABEL org.label-schema.vcs-ref=$SOURCE_COMMIT

ENV RABBITMQ_CONFIG_FILE=/var/lib/rabbitmq/rabbitmq.conf
ENV RABBITMQ_ADVANCED_CONFIG_FILE=/var/lib/rabbitmq/advanced.config

COPY *.json /etc/rabbitmq/

RUN rabbitmq-plugins enable --offline rabbitmq_federation rabbitmq_federation_management rabbitmq_shovel rabbitmq_shovel_management

COPY --chmod=755 docker-entrypoint.sh /usr/local/bin/

ENTRYPOINT ["docker-entrypoint.sh"]

CMD ["rabbitmq-server"]
