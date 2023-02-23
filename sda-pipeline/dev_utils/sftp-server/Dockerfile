FROM ubuntu:latest

RUN apt-get update && apt-get install -y \
--no-install-recommends \
    sudo openssh-server \
    && apt-get clean

COPY sshd_config /etc/ssh/sshd_config

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 22

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
