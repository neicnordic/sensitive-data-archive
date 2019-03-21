FROM rabbitmq:3.6-management-alpine

ARG BUILD_DATE
ARG SOURCE_COMMIT

LABEL maintainer "EGA System Developers"
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=$BUILD_DATE
LABEL org.label-schema.vcs-url="https://github.com/EGA-archive/LocalEGA-mq"
LABEL org.label-schema.vcs-ref=$SOURCE_COMMIT

EXPOSE 5672 15672

VOLUME /var/lib/rabbitmq

RUN mkdir -p /etc/rabbitmq/ && \
    chown -R rabbitmq:rabbitmq /etc/rabbitmq

# Initialization
RUN rabbitmq-plugins enable --offline rabbitmq_federation            && \
    rabbitmq-plugins enable --offline rabbitmq_federation_management && \
    rabbitmq-plugins enable --offline rabbitmq_shovel                && \
    rabbitmq-plugins enable --offline rabbitmq_shovel_management

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod 755 /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["rabbitmq-server"]
