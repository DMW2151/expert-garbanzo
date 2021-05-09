package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

var layerDir = os.Getenv("TILE_DIRECTORY")

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Write(
		[]byte("Good Morning, Helsinki!"),
	)
}

func getTile(w http.ResponseWriter, r *http.Request) {

	// Set Allow Origin && Define as Protobuf
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/x-protobuf")

	rV := mux.Vars(r)

	// Take the Incoming Request; Parse for X,Y,Z values
	Z, X, Y := rV["z"], rV["x"], rV["y"]
	layerName, ok := rV["layer"]

	if !ok {
		log.WithFields(
			log.Fields{
				"X": X, "Y": Y, "Z": Z, "Layer": layerName,
			},
		).Error("Request Missing Layer")
	}

	// Access the file on Disk -> ErrorCheck -> Back to the Writer
	file, err := os.Open(
		fmt.Sprintf("%s/%s/%s/%s/%s.pbf", layerDir, layerName, Z, X, Y),
	)

	if err != nil {
		log.WithFields(
			log.Fields{
				"X": X, "Y": Y, "Z": Z, "Layer": layerName,
			},
		).Warn("Failed to Access Tile")
		return
	}

	// NOTE; this assumes that we're serving storing and serving UNCOMPRESSED
	// TILES this isn't so efficient, but openlayers can't unzip on the client
	// side so just take the hit...
	_, err = io.Copy(w, file)

	if err != nil {
		log.WithFields(
			log.Fields{
				"X": X, "Y": Y, "Z": Z, "Layer": layerName,
			},
		).Warn("Failed Write Tile to Client")
	}

}

func init() {
	// Set Logging Config
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)

	// Adds significant overhead (20% w. Go 1.6)
	// might be better now...writing as of April 2021, Go 1.16
	log.SetReportCaller(false)

	log.SetFormatter(&log.TextFormatter{
		DisableColors:   true,
		TimestampFormat: "2006-01-02 15:04:05.0000",
	})
}

func main() {

	router := mux.NewRouter().StrictSlash(true)

	// Healthcheck the API...
	router.HandleFunc("/health/", healthCheck).Methods("GET")

	// Access the Tiles on Disk; Serve them to a Webpage
	router.HandleFunc("/{layer}/{z}/{x}/{y}", getTile).Methods("GET")

	log.Fatal(
		http.ListenAndServe(":2151", router),
	)
}
