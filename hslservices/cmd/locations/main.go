package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	hsl "github.com/dmw2151/hsldatabridge"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/mmcloughlin/geohash"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

var (
	ctx = context.Background()

	// NOTE: Replace this with a Real Number of N Max Connections (currently static @ 100)
	apiHandler = LocationsAPIHandler{
		client:      hsl.InitRedisClient(ctx),
		conns:       make([]*upgradedLocationListener, 100),
		sem:         semaphore.NewWeighted(100),
		mu:          sync.Mutex{},
		openIdx:     0,
		unregisterC: make(chan int),
	}

	// Upgrader for WS connections...
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

// LocationsAPIHandler - Responsible for Responding Web <-> LocationsAPI
// <-> Redis requests made to the Locations API endpoints
type LocationsAPIHandler struct {
	client      *redis.Client
	conns       []*upgradedLocationListener
	sem         *semaphore.Weighted
	mu          sync.Mutex
	openIdx     int
	unregisterC chan int
}

// upgradedLocationListener to avoid any blocking on message fanout to client
type upgradedLocationListener struct {
	c  *websocket.Conn
	cH chan []byte
}

// Healthcheck - Nothing More...
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Write(
		[]byte("Good Morning, Helsinki!"),
	)
}

// httpConnectionUpgrade - To Initialize a Websocket Connection need an upgrade
// function to hijack the original HTTP call, in this case, it's just adding the
// connection to LocationsAPIHandler list of registered connections
func (lh *LocationsAPIHandler) httpConnectionUpgrade(conn *websocket.Conn) {

	// Acquire lock so we can handle for the case where many clients attempt
	// at once, access the lock, pretty sure there's a
	// smarter way to do this connection pooling....

	if lh.sem.TryAcquire(1) {
		lh.mu.Lock()

		ull := &upgradedLocationListener{
			c:  conn,
			cH: make(chan []byte, 10), // Spare them a tiny buffer for each connection to handle for bursts
		}

		lh.conns[lh.openIdx] = ull

		lh.mu.Unlock()

		if err := ull.recv(lh.openIdx, lh.unregisterC); err != nil {
			lh.sem.Release(1)
			conn.Close()
			ull = nil
		}
	} else {
		// NOTE: Write some Connection Overload Error
		conn.WriteMessage(1, []byte("We're At Capacity"))
	}
}

// recv - receive messages to each client forever...
func (ull *upgradedLocationListener) recv(idx int, callbackCh chan int) error {
	for msg := range ull.cH {

		if err := ull.c.WriteMessage(1, msg); err != nil {

			// If the connection drops; remove the conn by sending the index of the channel
			// to the remove connection handler...
			if errors.Is(err, syscall.EPIPE) {
				log.Infof("Sending Unregister %d", idx)
				callbackCh <- idx
				return err
			}
		}
	}

	// Should never happen...Make up an error for here...
	return nil
}

// subscriptionFanout - subscribe to a topic (Redis PUB/SUB) channel and receive
// messages for perpetuity.
//
// For each connection registered on the LocationsAPIHandler, push the message
// along to that connection as well
func (lh *LocationsAPIHandler) subscriptionFanout() {

	sub := lh.client.Subscribe(
		lh.client.Context(), "currentLocationsPS",
	)

	defer func() {
		log.Info("Exit from PUB/SUB Channel")
		sub.Unsubscribe(lh.client.Context(), "currentLocationsPS")
	}()

	// Open Redis PUB/SUB Channel...
	channel := sub.Channel()
	log.Info("Reading from PUB/SUB Channel")

	for msg := range channel {
		// cast msg -> msgB and then send to all listening connections...
		if len(lh.conns) == 0 {
			continue // Save some $$$/CPU
		}

		msgB := []byte(msg.Payload)

		for i, sub := range lh.conns {
			if sub != nil {
				// Never Block!!
				select {
				case sub.cH <- msgB:
				default:
				}
			} else if i < lh.openIdx {
				lh.openIdx = i
			}
		}
	}
	log.Info("Exit from PUB/SUB Channel")
}

func (lh *LocationsAPIHandler) unregisterConnections() {

	for i := range lh.unregisterC {
		log.Infof("Got Unregister Command: %d", i)
		if i > lh.openIdx {
			lh.openIdx = i
		}
	}
}

// livelocationsHandler -
func (lh *LocationsAPIHandler) livelocationsHandler(w http.ResponseWriter, r *http.Request) {

	upgrader.CheckOrigin = func(r *http.Request) bool {
		return true
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Connection", "keep-alive")

	// upgrade this connection to a WebSocket connection
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error(err)
	}

	// Note - Check how this behaves on failure...
	lh.httpConnectionUpgrade(ws)

}

func (lh *LocationsAPIHandler) historicallocationsHandler(w http.ResponseWriter, r *http.Request) {

	var e = &hsl.Event{}
	// Take the Incoming Request; Parse into an event...
	err := json.NewDecoder(r.Body).Decode(&e)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	journeyID := e.GetEventHash()

	// TS.MRANGE uses a 5x nested structure for anything, wooof
	result, err := lh.client.Do(
		lh.client.Context(), "TS.MRANGE", "-", "+", "FILTER", fmt.Sprintf("journey=%s", journeyID),
	).Result()

	if err != nil {
		log.Error(err)
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Connection", "keep-alive")

	// What a Monstrosity; to make this efficient; need to know
	// length of array first, or limit to N spaces...e.g. last 240 positions == 60 min...

	var (
		respArr = make([]hsl.Event, 240)
		realLen int
	)

	for _, body := range (result).([]interface{}) {
		keys := (body).([]interface{})
		series, positions := keys[0].(string), keys[2]

		positionsArr := positions.([]interface{})
		realLen = len(positionsArr)

		if strings.HasSuffix(series, "gh:agg") {
			for i, tup := range positionsArr {
				ts, gh := tup.([]interface{})[0].(int64), tup.([]interface{})[1].(string)
				ghI, err := strconv.ParseFloat(gh, 64)

				if err != nil {
					log.Error(err)
				}

				lat, lng := geohash.DecodeIntWithPrecision(uint64(ghI), 64)

				if i > 240 { // Ensure no Fail if len > 240
					respArr[i] = hsl.Event{
						Lat:       lat,
						Lng:       lng,
						Timestamp: ts,
					}
				}
			}
		}

		if strings.HasSuffix(series, "speed:agg") {
			for i, tup := range positionsArr {
				spd := tup.([]interface{})[1].(string)
				spdf, err := strconv.ParseFloat(spd, 32)

				if err != nil {
					log.Error(err)
				}

				respArr[i].Spd = float32(spdf)
			}
		}
	}

	b, _ := json.Marshal(respArr[:realLen])
	w.Write(b)
}

func init() {

	// Set Logging Config
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)

	log.SetFormatter(&log.TextFormatter{
		DisableColors:   true,
		TimestampFormat: "2006-01-02 15:04:05.0000",
	})

	// Initialize the LocationsAPI Handler & Have It Subscribe
	// to Target Topics

	go apiHandler.subscriptionFanout()
	go apiHandler.unregisterConnections()
}

func main() {

	router := mux.NewRouter().StrictSlash(true)

	// Healthcheck the API...
	router.HandleFunc("/health/", healthCheck)

	// Live Locations Endpoint...
	router.HandleFunc("/locations/", apiHandler.livelocationsHandler)

	// Historical Locations Endpoint...
	router.HandleFunc("/histlocations/", apiHandler.historicallocationsHandler)

	log.Fatal(
		http.ListenAndServe(":2152", router),
	)
}
