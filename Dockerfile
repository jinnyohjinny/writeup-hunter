FROM golang:1.21-alpine

RUN apk add --no-cache git bash

WORKDIR /app

COPY . .

RUN go mod download

RUN chmod +x run.sh

CMD ["./run.sh"]