package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	hsl "github.com/dmw2151/hsldatabridge"
	redis "github.com/go-redis/redis/v8"
	"github.com/mmcloughlin/geohash"
	log "github.com/sirupsen/logrus"
)

var (
	msgBroker   = hsl.NewMsgBroker(1024)
	ctx, cancel = context.WithCancel(context.Background())
	_           = hsl.InitMQTTClient(msgBroker)
	redisClient = hsl.InitRedisClient(ctx)
	nWorkers    = 10 // Set Variable for System CPU cap...
)

// statJourneyID checks if a journeyID already exists in the set of previously
// seen JourneyID; attempts to SADD. Returns True if journey exists....
func statJourneyID(client *redis.Client, key string, journeyID string) bool {

	resp, err := client.Do(
		ctx, "SADD", key, journeyID,
	).Result()

	if err != nil {
		return false
	}

	// If resp == 0; then already exists...
	return resp.(int64) == 0
}

// createTimeSeriesPair - create a timeseries of events and maps it to
// auto-update a secondary time series with a compaction rule...
//
// WARNING: by default this setup ONLY allows for mapping 1:1 src to target
// event timeseries, should consider using something better to customize rules
func createTimeSeriesPair(client *redis.Client, journeyID string, label string) {

	// Initialize Creation Pipeline For a Statistic
	pipe := client.TxPipeline()

	// Create Parent && Child Series
	pipe.Do(
		ctx, "TS.CREATE", fmt.Sprintf("positions:%s:%s", journeyID, label),
	)

	pipe.Do(
		ctx, "TS.CREATE", fmt.Sprintf("positions:%s:%s:agg", journeyID, label),
		"RETENTION", 120*60*1000, "LABELS", label, 1, "journey", journeyID,
	)

	_, err := pipe.Exec(ctx)

	if err != nil {
		log.WithFields(
			log.Fields{
				"JourneyID":   journeyID,
				"Series":      fmt.Sprintf("positions:%s:%s", journeyID, label),
				"ChildSeries": fmt.Sprintf("positions:%s:%s:agg", journeyID, label),
			},
		).Warn("Create TimeSeries Root Series Failed: ", err)
	}

	// Using a second pipe, create a rule, split into 2 stages to ensure parent && child
	// series exist first....
	pipe.Do(
		ctx, "TS.CREATERULE",
		fmt.Sprintf("positions:%s:%s", journeyID, label),
		fmt.Sprintf("positions:%s:%s:agg", journeyID, label),
		"AGGREGATION", "LAST", 15000,
	)

	_, err = pipe.Exec(ctx)

	if err != nil {
		log.WithFields(
			log.Fields{
				"JourneyID":   journeyID,
				"Series":      fmt.Sprintf("positions:%s:%s", journeyID, label),
				"ChildSeries": fmt.Sprintf("positions:%s:%s:agg", journeyID, label),
			},
		).Warn("Create TimeSeries Pair Failed: ", err)
	}
}

// Launch some workers here...
func writeRedis(ctx context.Context, C <-chan []byte, client *redis.Client) {

	for msg := range C {

		// Receive the content of the MQTT message and de-serialize bytes into
		// struct
		e := &hsl.EventHolder{}
		err := hsl.DeserializeMQTTBody(msg, e)

		if err != nil {
			switch err := err.(type) {
			case *hsl.MQTTValidationError:

				// Most common error is Missing or Bad Coords; See defn for
				// `hsl.MQTTValidationError` for more...
				log.WithFields(log.Fields{"Body": e}).Debug("%+v", err)

			default:
				// The entry was not deserializable into a known msg types
				// Most often an error from the source feed, e.g the feed published
				// a route as 123 instead of "123", fail to unmarshal string into Go
				log.WithFields(log.Fields{"Body": e}).Debug("%+v", err)
			}

			continue
		}

		// Main procedure for adding a series keys, values to the redis
		// instance
		// MEMOIZE!!
		journeyID := e.VP.GetEventHash()

		// Check if JourneyID is known...
		journeyExists := statJourneyID(client, "journeyID", journeyID)

		// if not...then create the timeseries pair for the journey...
		if !(journeyExists) {

			log.WithFields(
				log.Fields{
					"JourneyID": journeyID,
				},
			).Info("New Journey Registered")

			createTimeSeriesPair(client, journeyID, "speed")
			createTimeSeriesPair(client, journeyID, "gh")
		}

		// Write The incoming event to multiple locations using
		// a single client Tx pipeline, cuts back on some network
		// round-trip
		pipe := client.TxPipeline()

		// 1. Publish full body...
		pipe.Publish(
			ctx, "currentLocationsPS", msg,
		)

		// 2. XADD the full event body to a stream of events, these
		// are swept up by a gears function and written behind to a DB
		// every XXXXms
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

		// 3. TS.ADD a series of statistics to the timeseries created
		// by `createTimeSeriesPair`
		pipe.Do(
			ctx,
			"TS.ADD", fmt.Sprintf("positions:%s:speed", journeyID),
			"*",
			e.VP.Spd,
			"RETENTION", 60*1000,
			"CHUNK_SIZE", 16,
			"ON_DUPLICATE", "LAST",
		)

		pipe.Do(
			ctx,
			"TS.ADD", fmt.Sprintf("positions:%s:gh", journeyID),
			"*",
			geohash.EncodeIntWithPrecision(e.VP.Lat, e.VP.Lng, 64),
			"RETENTION", 60*1000,
			"ON_DUPLICATE", "LAST",
		)

		// Execute Pipe!
		_, err = pipe.Exec(ctx)

		// Failed to Write an Event
		if err != nil {

			if err, ok := err.(net.Error); ok {
				log.Errorf("Redis Down: %+v", err)
			}

			log.WithFields(
				log.Fields{
					"Body": fmt.Sprintf("positions:%s:*", journeyID),
				},
			).Errorf("Failed to Write Event: %+v", err)

		} else {

			log.WithFields(
				log.Fields{"Journey": journeyID},
			).Debug("Wrote Event")

		}
	}
}

func init() {
	// Log as JSON instead of the default ASCII formatter.
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	log.SetLevel(log.WarnLevel)
}

func main() {

	quitChannel := make(chan os.Signal, 1)

	// Start Staging Channel -> Redis Workers
	for i := 0; i < nWorkers; i++ {
		go writeRedis(ctx, msgBroker.StagingC, redisClient)
	}

	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)
	<-quitChannel

}
