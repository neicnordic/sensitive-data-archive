ARG GOLANG_VERSION=1.20
FROM "golang:${GOLANG_VERSION}-alpine"
RUN apk add --no-cache git
COPY . .
ENV GO111MODULE=on
ENV GOPATH=$PWD
ENV CGO_ENABLED=0
ENV GOOS=linux
RUN go build -ldflags "-extldflags -static" -o ./build/svc .
RUN echo "nobody:x:65534:65534:nobody:/:/sbin/nologin" > passwd

FROM scratch
COPY --from=0 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=0 /go/build/svc svc
COPY --from=0 /go/frontend frontend/
COPY --from=0 /go/passwd /etc/passwd
USER 65534
EXPOSE 8080
ENTRYPOINT [ "/svc" ]

