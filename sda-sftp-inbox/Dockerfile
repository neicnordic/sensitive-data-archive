FROM maven:3-eclipse-temurin-22-alpine as builder

COPY pom.xml .

RUN mvn dependency:go-offline --no-transfer-progress

COPY src/ /src/

RUN mvn clean install -DskipTests --no-transfer-progress

FROM eclipse-temurin:21-alpine

RUN apk add --no-cache --upgrade ca-certificates java-cacerts libssl3 libcrypto3 \
    && ln -sf /etc/ssl/certs/java/cacerts $JAVA_HOME/lib/security/cacerts

RUN addgroup -g 1000 lega && \
    adduser -D -u 1000 -G lega lega

RUN mkdir -p /ega/inbox && \
    chown lega:lega /ega/inbox && \
    chmod 2770 /ega/inbox

VOLUME /ega/inbox

COPY --from=builder /target/inbox-0.0.3-SNAPSHOT.jar .

COPY entrypoint.sh .

RUN chmod +x entrypoint.sh

#USER 1000

# ENTRYPOINT ["/entrypoint.sh"]

CMD ["java", "-jar", "inbox-0.0.3-SNAPSHOT.jar"]
