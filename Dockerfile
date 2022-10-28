FROM node:19.0-buster
WORKDIR /usr/src/app
COPY package.json package-lock.json ./
RUN apk add --no-cache git
RUN npm ci --production
RUN npm cache clean --force
RUN npm install --save-dev smee-client
ENV NODE_ENV="production"
COPY . .
CMD [ "npm", "start" ]
