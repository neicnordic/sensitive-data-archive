FROM maven:3-eclipse-temurin-21-alpine as builder

COPY pom.xml .

RUN mkdir -p /root/.m2 && \
    mkdir /root/.m2/repository

COPY settings.xml /root/.m2

RUN mvn dependency:go-offline --no-transfer-progress

COPY src/ /src/

RUN mvn clean install -DskipTests --no-transfer-progress

FROM eclipse-temurin:21-jre-alpine

RUN apk -U upgrade && \
    apk add ca-certificates openssl && \
    rm -rf /var/cache/apk/*

COPY --from=builder /target/localega-doa-*.jar /localega-doa.jar

COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/docker-entrypoint.sh

USER 65534

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

CMD ["java", "-jar", "/localega-doa.jar"]
