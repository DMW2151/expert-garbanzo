CREATE EXTENSION if not exists postgis; -- not sure why this didn't load?

CREATE SCHEMA statistics;

CREATE TABLE statistics.events (
    id UUID NOT NULL, 
    approx_event_time timestamp DEFAULT now(),
    body JSONB NOT NULL,
    PRIMARY KEY (id)
);

CREATE INDEX ON statistics.events USING btree(approx_event_time);

create table if not exists statistics.trips_raw (
    route_id varchar,
    service_id varchar,
    trip_id varchar,
    trip_headsign varchar,
    direction_id int,
    shape_id varchar,
    wheelchair_accessible int,
    bikes_allowed int,
    max_delay int,
    primary key (trip_id)
);

create table if not exists statistics.routes_raw (
    shape_id varchar,
    lat numeric,
    lng numeric,
    shp_seq int,
    shape_dist numeric,
    primary key (shape_id, shp_seq)
);

create table if not exists statistics.stops_raw (
    stop_id varchar primary key,
    stop_code varchar,
    stop_name varchar,
    stop_desc varchar,
    stop_lat numeric,
    stop_lon numeric,
    zone_id varchar,
    stop_url varchar,
    location_type int,
    parent_station varchar,
    wheelchair_boarding int,
    platform_code varchar,
    vehicle_type int
);


create materialized view event_log as (
    select
        id,
        approx_event_time,
        st_setsrid(
        	st_makepoint(
	            (body ->> 'lng')::numeric, (body ->> 'lat')::numeric
	        ),
	        4326
	    ) as geom,
        (body ->> 'acc')::numeric as acc,
        (body ->> 'dl')::numeric as dl,
        (body ->> 'spd')::numeric as spd
    from "statistics"."events"
    where ((now() at time zone 'utc') - approx_event_time) < interval'1 hours'
);

create index on event_log using gist(geom);

