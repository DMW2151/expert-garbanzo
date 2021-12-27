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

// Launch some workers here...
func writeRedis(ctx context.Context, C <-chan []byte, client *redis.Client) {

	for msg := range C {

		// Receive the content of the MQTT message and de-serialize bytes
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

		// Write The incoming event
		journeyID := e.VP.GetEventHash()
		pipe := client.TxPipeline()

		pipe.Do(
			ctx,
			"SET",
			fmt.Sprintf("positions:%s:%d", e.VP.RouteID, e.VP.VehID),
			geohash.EncodeIntWithPrecision(e.VP.Lat, e.VP.Lng, 64),
			"EX",
			"600",
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
