#!/bin/bash
set -e

addgroup sftp

mkdir -pv /uploads/"$SFTP_USER"
mkdir -pv /uploads/"$SFTP_USER"/upload
useradd -d /uploads/"$SFTP_USER" -G sftp "$SFTP_USER" -s /usr/sbin/nologin
echo "$SFTP_USER:$SFTP_USER_PASS" | sudo chpasswd
chown "$SFTP_USER":sftp -R /uploads/"$SFTP_USER"/upload


# prepare public key authentication
mkdir -pv /uploads/"$SFTP_USER"/.ssh
sudo chown "$SFTP_USER":sftp /uploads/"$SFTP_USER"/.ssh
sudo chmod 0700 /uploads/"$SFTP_USER"/.ssh

touch /uploads/"$SFTP_USER"/.ssh/authorized_keys
cat "$SFTP_USER_PUB_KEY" > /uploads/"$SFTP_USER"/.ssh/authorized_keys

printf "\n\033[0;44mStarting the SSH server.\033[0m\n"
service ssh start
service ssh status

tail -f /dev/null
