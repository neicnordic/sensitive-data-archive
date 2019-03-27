FROM rabbitmq:3.7.8-management-alpine

RUN rabbitmq-plugins enable --offline rabbitmq_federation rabbitmq_federation_management rabbitmq_shovel rabbitmq_shovel_management

COPY entrypoint.sh /usr/local/bin/ega-entrypoint.sh

RUN chmod +x /usr/local/bin/ega-entrypoint.sh

COPY definitions.json /etc/rabbitmq/definitions.json

COPY advanced.config /etc/rabbitmq/advanced.config

COPY rabbitmq.conf /etc/rabbitmq/rabbitmq.conf

ENTRYPOINT ["/usr/local/bin/ega-entrypoint.sh"]

CMD ["rabbitmq-server"]
