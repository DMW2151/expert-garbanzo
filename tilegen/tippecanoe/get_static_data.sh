
## Download Helsinki GTFS Data && Helsinki Static Neighborhood Files
mkdir -p /sources/static/gtfs/ /sources/static/areas/

wget https://infopalvelut.storage.hsldev.com/gtfs/hsl.zip &&\
unzip hsl.zip -d /sources/static/gtfs/

wget https://avoidatastr.blob.core.windows.net/avoindata/AvoinData/9_Kartat/PKS%20postinumeroalueet/Shp/PKS_Postinumeroalueet_2020.shp.zip &&\
    unzip PKS_Postinumeroalueet_2020.shp.zip -d /sources/static/areas/

## Write Data to DB (ogr2ogr for SHP, GTFS as Table w. psql)
psql -h ${POSTGRES_HOST} \
    -U  ${POSTGRES_USER} \
    -d ${POSTGRES_DB} \
    -c "\copy statistics.stops_raw from '/sources/static/gtfs/stops.txt' WITH CSV HEADER DELIMITER ',';"

psql -h ${POSTGRES_HOST} \
    -U  ${POSTGRES_USER} \
    -d ${POSTGRES_DB} \
    -c "\copy statistics.routes_raw from '/sources/static/gtfs/shapes.txt' WITH CSV HEADER DELIMITER ',';"

ogr2ogr -a_srs "EPSG:4326" \
    -f "PostgreSQL" \
    -t_srs "EPSG:4326" \
    PG:"host=${POSTGRES_HOST} user=${POSTGRES_USER}  dbname=${POSTGRES_DB}" \
    /sources/static/areas/ \
    -nlt PROMOTE_TO_MULTI \
    -nln helsinki_places

## Write Data Out of DB -> GeoJSON -> Tiles 
rm -f /sources/static/gtfs/stops.geojson &&\
    ogr2ogr -f GeoJSON /sources/static/gtfs/stops.geojson \
        "PG:host=${POSTGRES_HOST} dbname=${POSTGRES_DB} user=${POSTGRES_USER}" \
        -nlt PROMOTE_TO_MULTI \
        -geomfield geom \
        -sql """
            select
                stop_id,
                stop_code,
                stop_name,
                zone_id,
                st_setsrid(st_makepoint(stop_lon, stop_lat), 4326) as geom
            from statistics.stops_raw;
        """

rm -f /sources/static/gtfs/routes.geojson &&\
    ogr2ogr -f GeoJSON /sources/static/gtfs/routes.geojson \
        "PG:host=${POSTGRES_HOST} dbname=${POSTGRES_DB} user=${POSTGRES_USER}" \
        -nlt PROMOTE_TO_MULTI \
        -geomfield geom \
        -sql """
            SELECT
                shape_id,
                ST_MakeLine(
                    st_setsrid(st_makepoint(lng, lat), 4326) ORDER BY shp_seq
                ) AS geom
            FROM statistics.routes_raw
            GROUP BY shape_id;
        """

## Tile Generation
mkdir -p /tiles/stops /tiles/routes/

tippecanoe -L stops:/sources/static/gtfs/stops.geojson \
    --no-tile-compression \
    --force \
    -e \
    /tiles/stops

tippecanoe -L routes:/sources/static/gtfs/routes.geojson \
    --no-tile-compression \
    --force \
    -e \
    /tiles/routes

