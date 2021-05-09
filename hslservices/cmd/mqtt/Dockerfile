
ARG BUILDPATH='/build/hsl/cmd'

FROM golang:1.16-alpine as builder

ARG BUILDPATH

WORKDIR $BUILDPATH

# Copy code in && fetch dependencies
COPY . ./
RUN go mod download

# Build the MQTT > Redis Connector (produces an image of ~1.02GB, 900GB of it from Go 1.16)
RUN cd ./cmd/mqtt &&\
    CGO_ENABLED=1 go build -o mqttconnector

# Create final image -  Just Export Go Binary onto regular alpine w. mqtt -> ~10MB
FROM alpine:latest

ARG BUILDPATH

RUN apk add --no-cache --purge mosquitto-libs mosquitto-clients &&\
     rm -rf /var/cache/apk/* /tmp/*
    
COPY --from=builder $BUILDPATH .

CMD ["./cmd/mqtt/mqttconnector"]
