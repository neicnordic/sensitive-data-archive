FROM rabbitmq:3.6.14-management

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
