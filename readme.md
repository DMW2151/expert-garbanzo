# Helsinki Transit System - Live Tracking w. Redis

This project publishes realtime locations of municipal transport vehicles in the Helsinki metro area to a Web UI. Although Helsinki offers a great [realtime API](https://digitransit.fi/en/developers/apis/4-realtime-api/vehicle-positions/) for developers, there is no such site that makes this data generally available to the public.<sup>1</sup>

All components of the app are hosted on single AWS `t3.medium` with a `gp3` EBS volume and is running live at https://maphub.dev/helsinki


![Screenshot of Live Map - Downtown Helsinki](https://github.com/DMW2151/expert-garbanzo/blob/master/docs/live_.png)

UI with **GTFS** (black) and **live-location** (blue) layers enabled.

![Screenshot of Live Map - Neighborhoods](https://github.com/DMW2151/expert-garbanzo/blob/master/docs/areas.png)

UI with the **current traffic** layer enabled - an hourly summary is aggregated to the neighborhood level and then colored based on vehicle speed in the area.

![Screenshot of Live Map - History of Single Vehicle](https://github.com/DMW2151/expert-garbanzo/blob/master/docs/Single.png)

UI with the **trip history** layer and tooltip showing details of vehicle's current status. Coloring maps to vehicle's contemporaneous speed.

-------

- [Helsinki Transit System - Live Tracking w. Redis](#Helsinki-Transit-System---Live-Tracking-w-Redis)
  - [Summary](#Summary)
  - [Local Build - Startup Notes](#Local-Build---Startup-Notes)
  - [System Architecture](#System-Architecture)
    - [Ingesting Data w. MQTT to Redis Broker](#Ingesting-Data-w-MQTT-to-Redis-Broker)
      - [Writing Data to PubSub Channel](#Writing-Data-to-PubSub-Channel)
      - [Writing Data to Event Stream](#Writing-Data-to-Event-Stream)
      - [Writing Data to TimeSeries](#Writing-Data-to-TimeSeries)
        - [Commands](#Commands)
    - [Redis Gears](#Redis-Gears)
    - [Tile Generation Pipeline (PostGIS)](#Tile-Generation-Pipeline-PostGIS)
    - [Accessing Data with the Locations API](#Accessing-Data-with-the-Locations-API)
      - [Commands](#Commands-1)
    - [Frontend](#Frontend)
  - [Technical Appendix](#Technical-Appendix)
    - [Data Throughput](#Data-Throughput)
    - [CPU and Disk Usage](#CPU-and-Disk-Usage)
  
_______

## Summary

Data is sourced from the Helsinki Regional Transit Authority via a public [MQTT feed](https://digitransit.fi/en/developers/apis/4-realtime-api/vehicle-positions/). Incoming MQTT messages are processed through a [custom MQTT broker](./hslservices/cmd/mqtt/main.go) that pushes them to Redis.

MQTT messages are delivered in 2 parts, message topic and message body. Consider the example message below for demonstrations sake:

```bash
# Topic - Delivered as Msg Part 1
/hfp/v2/journey/ongoing/vp/bus/0018/00423/2159/2/Matinkylä (M)/09:32/2442201/3/60;24/16/58/67

# Body - Delivered as Msg Part 2
{
  "VP": {
    "desi": "159",
    "dir": "2",
    "oper": 6,
    "veh": 423,
    "tst": "2021-05-15T06:40:28.629Z",
    "tsi": 1621060828,
    "spd": 21.71,
    "hdg": 67,
    "lat": 60.156949,
    "long": 24.687111,
    "acc": 0,
    "dl": -21,
    "odo": null,
    "drst": null,
    "oday": "2021-05-15",
    "jrn": 202,
    "line": 1062,
    "start": "09:32",
    "loc": "GPS",
    "stop": null,
    "route": "2159",
    "occu": 0
  }
}
```

Once in Redis, the data is fanned out to a stream, a pub/sub channel, and multiple time series.

1. Event data sent to a stream is processed with a Redis Gears function and written to persistent storage (PostgreSQL). Once in PostgreSQL, this data is processed hourly and used to generate MapBox tiles for the **current traffic** layer.

2. Event data sent to the PUB/SUB channel is forwarded to each connected client via websocket. This allows for live updates of positions in the browser on the **live-location** layer.

3. Time series data is split into separate series for for position (geohash, represented as int) and speed for each scheduled trip. These timeseries are then compacted and served to the frontend by a Golang API which allows the user to access a **trip history** layer.

The remainder of this document will go through the application in a bit more detail, including local deployment of the application, Redis commands used in each stage & system traffic and architecture.

--------

## Local Build - Startup Notes

A functional version of the system can be spun up locally with `docker-compose`. This will spin up (almost) all services required to run a local demo in their own isolated environments.

```bash
docker-compose up --build
```

The following command can be run if you're interested in receiving periodic updates to the traffic speeds/neighborhoods layer. This is not strictly necessary as it can take several hours to gather sufficient data to get a reasonable amount of data (and you'd still need to wait to the `tilegen` job to come around to repopulate layers).

```bash
docker exec <name of redis container> \ # (e.g. redis_hackathon_redis_1)
    bash -c "gears-cli run /redis/stream_writebehind.py --requirements /redis/requirements.txt"
```

------

## System Architecture

![Arch](https://github.com/DMW2151/expert-garbanzo/blob/master/docs/arch.jpg)

### Ingesting Data w. MQTT to Redis Broker

The MQTT broker is a Golang service that subscribes to a MQTT feed provided by the Helsinki Transit Authority. This service pushes MQTT message data to Redis after processing the message. More about the real-time positioning data from the HSL Metro can be found [here](https://digitransit.fi/en/developers/apis/4-realtime-api/vehicle-positions/). The broker is responsible for writing an incoming message from the MQTT feed to each of the following locations:

#### Writing Data to PubSub Channel

The incoming event is published to a PUB/SUB channel in Redis. This component (the mqtt broker) uses Golang as a Redis client and uses the code/command below.

```golang
// In Golang...
pipe := client.TxPipeline()
ctx := client.Context()

// Stylizing the Actual Message Body for Readme
msg := &hsl.EventHolder{
    "acc": 0.1, "speed": 10.6, "route": "foo"
}

pipe.Publish(
    ctx, "currentLocationsPS", msg
)
```

```bash
# Using a standard Redis client...
127.0.0.1:6379>  PUBLISH currentLocationsPS '{"acc": 0.1, "speed": 10.6, "route": "foo"}'
```

#### Writing Data to Event Stream

The incoming event is pushed to a stream. This stream is later cleared and processed by code that runs via [Redis Gears](./redis/stream_writebehind.py). As with the PUB/SUB channel, this is written using the Redis Go Client shown below.

```golang
// In Golang...
pipe.XAdd(
    ctx, &redis.XAddArgs{
        Stream: "events",
        Values: []interface{}{
            "jid", journeyID,
            "lat", e.VP.Lat,
            "lng", e.VP.Lng,
            "time", e.VP.Timestamp,
            "spd", e.VP.Spd,
            "acc", e.VP.Acc,
            "dl", e.VP.DeltaToSchedule,
        },
    },
)
```

```bash
# Using a standard Redis client...
127.0.0.1:6379>  XADD events * jid journeyhashID lat 60 lng 25 time 1620533624765 speed 10 acc 0.1 dl "00:00"
```

#### Writing Data to TimeSeries

The incoming event is pushed to several time series. A unique identifier is created for each "trip" (referred to as **JourneyHash**) hashing certain attributes from the event. The broker creates a time series for both speed and location for each journeyhash. 

- Location data is stored in a time series by encoding a (lat, lng) position to an integer representation (much like Redis does internally for `GEO.XXX` commands).
  
- Speed data is simply stored as m/s, as it appears in the original MQTT message.

The position and speed series have a short retention and are compacted to secondary time series. These compacted series have a much longer retention time (~2hr) and are used by the API to show users the **trip history** layer. By quickly expiring/aggregating individual events, this pattern allows us to keep memory usage much lower.

##### Commands

As with previous sections, the commands are executed by Golang. As the standard Golang client does not include the `TS.XXX` commands, I will forgo showing the Go written for this section. 

First, I check to see if a journeyhash has not yet been seen by checking it's inclusion in a set (`journeyID`). If the following returns `1`, I proceed with creating series and rules, else, I just `TS.ADD` the data.

```bash
SADD journeyID <JOURNEYHASH>
```

The first series is created with the following command. For the remainder of this section, I'll refer to these as **Time Series A**

```bash
127.0.0.1:6379>  TS.CREATE positions:<JOURNEYHASH>:speed
127.0.0.1:6379>  TS.CREATE positions:<JOURNEYHASH>:gh
```

The aggregation series are fed by the "main" timeseries and created with the command below. I'll refer to these as **Time Series B**

```bash
127.0.0.1:6379>  TS.CREATE positions:<JOURNEYHASH>:speed:agg RETENTION 7200000 LABELS speed 1 journey <JOURNEYHASH>
127.0.0.1:6379>  TS.CREATE positions:<JOURNEYHASH>:gh:agg RETENTION 7200000 LABELS gh 1 journey <JOURNEYHASH>
```

For the rule that governs **Time Series A** -> **Time Series B**, I use the following command:

```bash
127.0.0.1:6379> TS.CREATERULE positions:<JOURNEYHASH>:speed positions:<JOURNEYHASH>:speed:agg AGGREGATION LAST 150000
127.0.0.1:6379> TS.CREATERULE positions:<JOURNEYHASH>:gh positions:<JOURNEYHASH>:gh:agg AGGREGATION LAST 150000
```

To add data to **Time Series A** I use the following:

```bash
127.0.0.1:6379> TS.ADD positions:<JOURNEYHASH>:speed * 10 RETENTION 60000 CHUNK_SIZE 16 ON_DUPLICATE LAST
127.0.0.1:6379> TS.ADD positions:<JOURNEYHASH>:gh * 123456123456163 RETENTION 60000 ON_DUPLICATE LAST
```

In the example above, `123456123456163` is a fake number which represents a integer encoding of a geohash coordinate to integer encoding was handled in Go with [this](https://pkg.go.dev/github.com/mmcloughlin/geohash@v0.10.0) package.

### Redis Gears

I use a Docker image that is almost identical to `redislabs/redismod:latest` (see: [Dockerfile](/redis/Dockerfile)) as the base image for this project. The only significant difference is that this container contains a RedisGears function which implements a write-behind pattern.

This function consumes from a stream and writes data to PostgreSQL/PostGIS every 5s/10,000 events. Even though Gears runs off the main thread, this function is designed to do minimal data-processing. This function simply dumps MQTT event data into PostGIS and allows the PostGIS and `Tilegen` processes to transform these events to MBtiles.

The RedisGears function is written in Python and doesn't call any Redis commands; See [function](/redis/stream_writebehind.py).

### Tile Generation Pipeline (PostGIS)

The `PostGIS` and `Tilegen` containers are crucial in serving **GTFS** and  **current traffic** layers.

PostGIS is a PostgreSQL extension that enables geospatial operations.

TileGen is an alpine container that contains two common utilities used in geospatial processing, `GDAL` and `tippecanoe` (and `psql`, the PostgreSQL client). This container is required for:

1. [Sourcing static data](tilegen/tippecanoe/get_static_data.sh) and pushing it to PostGIS with [GDAL](https://gdal.org/)
2. Periodic [regeneration of tiles](/tilegen/tippecanoe/tilegen.sh) using [Tippecanoe](https://github.com/mapbox/tippecanoe)

The TilesAPI is a  simple Golang API which is used to fetch those tiles from disk and send them to the frontend.

### Accessing Data with the Locations API

The Locations API has two endpoints `/locations/` and `/histlocations/`.

- `/locations/` subscribes to the Redis PUB/SUB channel described earlier. When a client connects to this endpoint, the connection is upgraded and events are pushed along to the client in real-time.
  
- `/histlocations/` queries a specific trip timeseries in Redis using `TS.MRANGE`; the API takes the "merged" result and creates a response of historical positions and speeds for a given trip.

#### Commands

The `/locations/` endpoint subscribes/reads data from the PUB/SUB channel defined in the MQTT broker section. While written in Go, the redis-cli command for this would be:

```bash
127.0.0.1:6379> SUBSCRIBE currentLocationsPS
```

The `/histlocations/` endpoint needs to gather data from multiple time series to create a combined response for the client, this means making a `TS.MRANGE` call. Because each **Timeseries B** is labelled with it's journey hash, the `TS.MRANGE` gathers the position and speed stats with a single call, filtering on journey hash.

```bash
127.0.0.1:6379> TS.MRANGE - + FILTER journey=<JOURNEYHASH>
```

### Frontend

The frontend uses [OpenLayers](https://openlayers.org/), a JS library, to create a map and display the layers created by the previously described services. In prodduction, this is served using Nginx rather than Parcel's development mode.

The frontend also makes calls to a publicly available [API](https://carto.com/help/building-maps/basemap-list/) for basemap imagery.

------

## Technical Appendix

### Data Throughput

This system is not explicitly architected to handle huge amounts of data, but it does perform acceptably given this (relatively small scale) task. Anecdotally, the system processes ~15GB of messages per day when subscribed to the MQTT topic corresponding to all bus position updates.

The following charts display the rise in event throughput on a Sunday morning into afternoon and evening. Notice that towards the middle of the day the events/second top out at 500/s (30k events/min shown on graph) after growing steadily from < 10 events/s (1k events/minute) early in the morning.

![Events](https://github.com/DMW2151/expert-garbanzo/blob/master/docs/events_epm_iii.png)

![Events](https://github.com/DMW2151/expert-garbanzo/blob/master/docs/events_per_min.png)

Alternatively, on a weekday morning at 8:00am, we can see the system handling ~1600+ events/s relatively comfortably. Consider the following stats from a five minute window the morning of 5/14/2021.

```sql
select
    now(), -- UTC
    count(1)/300 as eps -- averaged over prev 300s
from statistics.events
where approx_event_time > now() - interval'5 minute';


              now              | eps
-------------------------------+------
 2021-05-14 05:06:28.974982+00 | 1646
```

### CPU and Disk Usage

In local testing, I found the most stressed part of the system wasn't CPU as I had originally suspected, but instead the disk. Upgrading from AWS standard `gp2` EBS to `gp3` EBS allowed me to get 3000 IOPs and 125MB/s throughput essentially for free (<$1.50/month for this project) and made hosting the PostgreSQL instance in a container viable.

```bash
CONTAINER ID   NAME                     CPU %     MEM USAGE / LIMIT     MEM %     NET I/O
6d0a1d7fab0d   redis_hackathon_mqtt_1   24.02%    10.71MiB / 3.786GiB   0.28%     32GB / 60.4GB  
833aab4d39a8   redis_hackathon_redis_1  7.02%     862.7MiB / 3.786GiB   22.26%    58.8GB / 38.9GB
```

Prior to upgrade, system load was very high due to the write-behind from gears. with the update, even during rush-hour (decoding/encoding messages -> CPU Heavy) and tile regeneration (Both Disk & CPU heavy), `%iowait` stays low and system load stays < 1. Consider the following results from `sar` during a tile regeneration event

```bash
                CPU     %user   %system   %iowait   %idle  
20:00:07        all      9.98      9.88     47.08   32.56
# PostgreSQL Aggregations - Disk Heavy --
20:00:22        all     10.22     12.04     41.70   35.22
20:00:37        all     10.46     10.66     61.73   16.95
20:00:52        all     34.89     11.97     34.48   18.56
20:01:07        all      8.00      8.51     55.59   26.97
# Tilegeneration - User Heavy --
20:01:22        all     32.93      8.13     26.42   32.42
20:01:37        all     48.94     10.90     21.29   18.87
# Back to High Idle % --
20:01:47        all      7.19      4.39      5.89   81.24
```
------

<sup>1</sup> I am not a resident of Helsinki, my knowledge of the transit authority's product offerings is limited by both a lack of familiarity with the city and the Finnish language. 


