FROM postgres:18.3-alpine3.23

ARG BUILD_DATE
ARG SOURCE_COMMIT

# postgres:18 moved the default PGDATA to /var/lib/postgresql/18/docker,
# keep the layout that the compose files and the sda-db chart mount.
ENV PGDATA=/var/lib/postgresql/data
# owned by postgres like before 18, so a fresh volume works with --user postgres
RUN install -d -o postgres -g postgres -m 1777 "$PGDATA"

COPY initdb.d      /docker-entrypoint-initdb.d
COPY migratedb.d   /migratedb.d
COPY --chmod=755 entrypoint.sh /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

CMD ["postgres"]