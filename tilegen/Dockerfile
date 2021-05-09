FROM osgeo/gdal:alpine-small-latest

# Add PostgreSQL Client Binary
RUN apk add postgresql-client
 
# Add Tippecanoe Binary, sourced from: https://github.com/jskeates/docker-tippecanoe/blob/master/Dockerfile
ARG TIPPECANOE_RELEASE="1.36.0"

RUN apk add --no-cache git g++ make libgcc libstdc++ sqlite-libs sqlite-dev zlib-dev bash \
    && cd /root \
    && git clone https://github.com/mapbox/tippecanoe.git tippecanoe \
    && cd tippecanoe \
    && git checkout tags/$TIPPECANOE_RELEASE \
    && cd /root/tippecanoe \
    && make \
    && make install \
    && cd /root \
    && rm -rf /root/tippecanoe \
    && apk del git g++ make sqlite-dev

COPY ./tippecanoe /

RUN crontab crontab.txt

RUN chmod 755 /tilegen.sh /get_static_data.sh /docker-entrypoint.sh

CMD './docker-entrypoint.sh'