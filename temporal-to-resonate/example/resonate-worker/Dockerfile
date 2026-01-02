FROM node:20-slim

WORKDIR /app

COPY package.json ./
RUN npm install

COPY worker.ts ./

CMD ["npm", "start"]
