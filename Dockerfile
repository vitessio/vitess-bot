FROM node:19.0-buster

WORKDIR /usr/src/app

RUN wget https://dl.google.com/go/go1.20.linux-amd64.tar.gz && tar -xvf go1.20.linux-amd64.tar.gz && mv go /usr/local

ENV GOROOT=/usr/local/go
ENV GOPATH=$HOME/go
ENV PATH=$GOPATH/bin:$GOROOT/bin:$PATH

COPY package.json package-lock.json ./

RUN npm ci --production

RUN npm cache clean --force

RUN npm install --save-dev smee-client

ENV NODE_ENV="production"

COPY . .

CMD [ "npm", "start" ]
