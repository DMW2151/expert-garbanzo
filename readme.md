# MQTT -> Redis

This project uses MQTT, Golang, Redis, and Openlayers to publish live locations of vehicles in the Helsinki Metro Area.

## Demo Images

Screenshot of Live Map - Downtown Helsinki Around 3am Sunday Morning

![Live](./docs/live.png)

Screenshot of Live Map - Areas with Color Intensity Mapping to Average Area Speed

![Areas](./docs/areas.png)


Screenshot of Live Map - History of Single Vehicle, Darker Colors on Path -> Slower Speed

![Single](./docs/Single.png)

## Startup Notes

A functional version of the system can be spun up locally with `docker-compose up --build`. This will spin up (almost) all services required to run a local demo.

```bash
docker-compose up --build
```

The following command should be run if you're interested in receiving periodic updates to the traffic speeds/neighborhoods layer. This layer is optional, and has been left out of the main compose file as it takes a ~30 min of data collection before patterns begin to emerge.

```bash
docker exec redis_hackathon_redis_1 \
    bash -c "gears-cli run /redis/stream_writebehind.py --requirements /redis/requirements.txt"
```

## Architecture Diagram

![Arch](./docs/arch.jpg)

The following section will describe the architecture of the system

------------

### MQTT Broker

The MQTT broker is a Golang service that subscribes to a MQTT feed provided by the Helsinki Transit Authority and pushes that data to Redis. More about the real time positioning data from the HSL Metro can be found [here](https://digitransit.fi/en/developers/apis/4-realtime-api/vehicle-positions/).

The broker is responsible for writing an incoming message from the MQTT feed to each of the following locations:

1. PubSub Channel

   The incoming event is pushed to a pub/sub channel in Redis.

2. Stream

   The incoming event is pushed to a stream. This stream is later processed by code that runs via Redis Gears.

3. TimeSeries

   The incoming event is pushed to several time series. A unique identifier is created for each "trip" by combining and hashing certain attributes from the event. If they do not yet exist, the broker creates a time series for both speed and position. Speed and location data are then pushed to a separate series for each trip.

   Location data is stored in a time series by encoding (lat, lng) position from the event and encoding it to an integer representation of a geohash. The position and speed series have a very short retention span and are compacted to secondary time series that stores 15s aggregate values. These aggregate values have a much longer retention time (~2hr). This allows us to keep memory usage down


### Redis

I use a docker image that is based on `redislabs/redismod:latest`. I pre-install pip to smooth the process of running gears-cli calls in the container.

The container also contains a Gears function. When triggered, the function implements a write-behind pattern, consuming from a stream and writing data to PostgreSQL/PostGIS every 5s/10,000 events. Even though Gears runs off the main thread, this function is designed to do the minimal data-processing. This function simply dumps MQTT event data into PostGIS and allows the PostGIS and TileGen Containers to transform it to MBtiles.

### PostGIS

PostGIS is a PostgreSQL extension that enables geospatial operations. Static GTFS data is downloaded into PostGIS so that it can be later used to create the `routes` , `areas`, and `stops` layers. The PostGIS database is also the target of the Redis Gears function.

### TileGen

TileGen is an alpine container that contains two common utilities used in geospatial processing, `GDAL` and `tippecanoe` (and `psql`, the PostgreSQL client). This container is required for:

1. Sourcing static data and pushing it to PostGIS with `GDAL` (`./get_static_data.sh`)
2. Periodic regeneration of tiles using `tippecanoe` (`./tilegen.sh`)

You can read more about these two projects here:

- [GDAL](https://gdal.org/)
- [Tippecanoe](https://github.com/mapbox/tippecanoe)

### Tiles API

After TileGen has run, it produces a series of Mapbox Tiles saved on disk. The TilesAPI is a very simple Golang API which is used to fetch those tiles from disk and send them to the frontend.

### Locations API

The Locations API has two endpoints `/locations/` and `/histlocations/`.

- `/locations/` subscribes to the Redis PUB/SUB channel that we publish incoming events to. When a client connects to this endpoint, the connection is upgraded and events are pushed along to the client in real-time.
  
- `/histlocations/` queries the trip timeseries in redis using `MRANGE`, the API takes the "merged" response and creates a response of historical positions and speeds for a given trip.

### Frontend

The frontend uses [OpenLayers](https://openlayers.org/), a JS library, to create a map and display the layers created by the previously described services. 

- The GTFS Layer comes from the Tiles API
- The Live Layer comes from the Locations API
- The base map images come from calling a publicly available [Carto API](https://carto.com/help/building-maps/basemap-list/)