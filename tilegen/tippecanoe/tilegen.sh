#!/bin/sh
mkdir -p /sources/agg/

# Refresh the Materialized View Used to Generate the Layers...
psql -h ${POSTGRES_HOST} \
    -U ${POSTGRES_USER} \
    -d ${POSTGRES_DB} \
    -c """
    -- Huge Delete, Should trigger autovac daemon automatically; 
    -- This table could get big, w. dead tuples...
    
    DELETE FROM statistics.events 
    where approx_event_time < now() at time zone 'utc' - interval'2 hours';

    REFRESH MATERIALIZED VIEW event_log;
    """

# Create an Aggregate View of The last N hours (see defn from event_log)
rm -f /sources/agg/statistics.geojson  &&\
    ogr2ogr -f GeoJSON /sources/agg/statistics.geojson \
    "PG:host=${POSTGRES_HOST} dbname=${POSTGRES_DB} user=${POSTGRES_USER}" \
    -nlt PROMOTE_TO_MULTI \
    -geomfield geom \
    -sql """
        select 
            hp2.nimi,
            hp2.wkb_geometry as geom,
            stats.spd,
            stats.dl
        from (
            select	
                ogc_fid,
                avg(spd) as spd, 
                avg(dl) as dl 
            from helsinki_places hp 
            left join event_log
            on st_intersects(hp.wkb_geometry, event_log.geom)
            group by ogc_fid
        ) stats
        left join helsinki_places hp2 
        using (ogc_fid);
    """

# Write the Tiles out to the volume....
if [ "$(stat -c %s "/sources/agg/statistics.geojson")" -gt "100" ]; then
    tippecanoe -L statistics:/sources/agg/statistics.geojson \
        --no-tile-compression \
        --force \
        -e /tiles/statistics
fi;