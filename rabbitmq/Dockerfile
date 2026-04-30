FROM rabbitmq:4.3.0-management-alpine

ARG BUILD_DATE
ARG SOURCE_COMMIT


ENV RABBITMQ_CONFIG_FILE=/var/lib/rabbitmq/rabbitmq.conf
ENV RABBITMQ_ADVANCED_CONFIG_FILE=/var/lib/rabbitmq/advanced.config

COPY *.json /etc/rabbitmq/

RUN rabbitmq-plugins enable --offline rabbitmq_federation rabbitmq_federation_management rabbitmq_shovel rabbitmq_shovel_management

COPY --chmod=755 docker-entrypoint.sh /usr/local/bin/

ENTRYPOINT ["docker-entrypoint.sh"]

CMD ["rabbitmq-server"]
