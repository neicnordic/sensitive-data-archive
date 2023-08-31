FROM node:16.2.0-slim

WORKDIR /app

RUN apt update && apt upgrade -qy && apt install -qy curl

COPY package.json ./

RUN  rm -rf node_modules

RUN npm install -g npm@latest && \
    npm i camelcase oidc-provider

COPY . .

CMD [ "node", "server.js" ]
