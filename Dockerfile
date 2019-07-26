# Dockerfile References: https://docs.docker.com/engine/reference/builder/

# Start from golang v1.11 base image
FROM alpine:latest

# Add Maintainer Info
LABEL maintainer="Antonio De Iasio <antonio.deiasio@gmail.com>"

RUN mkdir /work

RUN apk add --no-cache bash

COPY glibc-2.29-r0.apk glibc-2.29-r0.apk

RUN apk --no-cache add ca-certificates wget && \
    wget -q -O /etc/apk/keys/sgerrand.rsa.pub https://alpine-pkgs.sgerrand.com/sgerrand.rsa.pub && \
    #wget https://github.com/sgerrand/alpine-pkg-glibc/releases/download/2.29-r0/glibc-2.29-r0.apk && \
    apk add glibc-2.29-r0.apk

# Set the Current Working Directory inside the container
WORKDIR /work

# Copy everything from the current directory to the PWD(Present Working Directory) inside the container
COPY master.etcd-client.key master.etcd-client.key
COPY master.etcd-ca.crt master.etcd-ca.crt
COPY master.etcd-client.crt master.etcd-client.crt
COPY synchronizer synchronizer
COPY startup.sh startup.sh

RUN chmod 777 -R /work
# Download all the dependencies
# https://stackoverflow.com/questions/28031603/what-do-three-dots-mean-in-go-command-line-invocations
#RUN go get -d -v ./...
# Install the package
#RUN go install -v ./...

RUN chmod +x startup.sh
RUN chmod +x synchronizer



# This container exposes port 8080 to the outside world
EXPOSE 2379

ENTRYPOINT ["./startup.sh"]
CMD ["sock-shop","user-db","100"]