FROM node:8-slim

LABEL maintainer="Jonathan Gros-Dubois"
LABEL version="14.0.4"
LABEL description="Docker file for SocketCluster with support for clustering."

RUN mkdir -p /usr/src/
WORKDIR /usr/src/
COPY . /usr/src/

ARG REDIS_HOST=localhost
ENV REDIS_HOST ${REDIS_HOST}
ARG REDIS_PASS=null
ENV REDIS_PASS ${REDIS_PASS}

RUN npm install .

EXPOSE 8000

CMD ["npm", "run", "start:docker"]
