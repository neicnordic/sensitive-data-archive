FROM postgres:13-alpine

# Note that this dockerfile needs to be built from the parent directory to
# be able to copy the initdb.d directory.

RUN apk add go

COPY ./initdb.d/      /docker-entrypoint-initdb.d
COPY ./tests/         /tests

RUN chmod 755 /tests/run_tests.sh
WORKDIR /tests
RUN go get .

USER 70

CMD ["/bin/bash", "run_tests.sh"]
