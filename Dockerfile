FROM golang:1.24.2-alpine3.21

RUN apk add --no-cache git bash

WORKDIR /app

COPY . .

RUN go mod download

RUN chmod +x run.sh

CMD ["./run.sh"]