FROM rabbitmq:3.7.8-management-alpine

ARG BUILD_DATE
ARG SOURCE_COMMIT

LABEL maintainer "EGA System Developers"
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=$BUILD_DATE
LABEL org.label-schema.vcs-url="https://github.com/EGA-archive/LocalEGA-mq"
LABEL org.label-schema.vcs-ref=$SOURCE_COMMIT

EXPOSE 5672 15672

VOLUME /var/lib/rabbitmq

RUN rabbitmq-plugins enable --offline rabbitmq_federation rabbitmq_federation_management rabbitmq_shovel rabbitmq_shovel_management

COPY entrypoint.sh /usr/local/bin/ega-entrypoint.sh

RUN chmod +x /usr/local/bin/ega-entrypoint.sh

COPY rabbitmq.conf /etc/rabbitmq/rabbitmq.conf

ENTRYPOINT ["/usr/local/bin/ega-entrypoint.sh"]

CMD ["rabbitmq-server"]
