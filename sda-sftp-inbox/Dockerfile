FROM maven:3.9.0-eclipse-temurin-19-alpine as builder

COPY pom.xml .

RUN mvn dependency:go-offline --no-transfer-progress

COPY src/ /src/

RUN mvn clean install -DskipTests --no-transfer-progress

FROM openjdk:19-alpine

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
