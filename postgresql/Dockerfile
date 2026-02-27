FROM postgres:15.16-alpine3.23

ARG BUILD_DATE
ARG SOURCE_COMMIT

COPY initdb.d      /docker-entrypoint-initdb.d
COPY migratedb.d   /migratedb.d
COPY --chmod=755 entrypoint.sh /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

CMD ["postgres"]