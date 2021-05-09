package hsldatabridge

import (
	"crypto/md5"
	"fmt"
	"io"
	log "github.com/sirupsen/logrus"
)

// Event - The Event body contains a single key that indicates event type, rather
// than designating a custom struct for each event, only store the relevent keys
// from any type. Refer to documentation at the below URL for full description
// of the response body.
// Docs: https://digitransit.fi/en/developers/apis/4-realtime-api/vehicle-positions/#event-types
type Event struct {
	JrnID           int     `json:"jrn"`   // Internal journey descriptor, not meant to be useful for external use.
	ODay            string  `json:"oday"`  // Operating day of the trip. The exact time when an operating day ends depends on the route.
	Direction       string  `json:"dir"`   // Route direction of the trip
	VehID           int     `json:"veh"`   // Vehicle number that can be seen painted on the side of the vehicle - Can be String OR Int
	Timestamp       int64   `json:"tsi"`   // UTC timestamp with millisecond precision from the vehicle in UnixTime
	Lat             float64 `json:"lat"`   // WGS 84 latitude in degrees.
	Lng             float64 `json:"long"`  // WGS 84 longitude in degrees.
	Heading         int     `json:"hdg"`   // Heading of the vehicle, in degrees (⁰) starting clockwise from geographic north.
	Start           string  `json:"start"` // Scheduled start time of the trip, i.e. the scheduled departure time from the first stop of the trip.
	DeltaToSchedule float32 `json:"dl"`    // Offset from the scheduled timetable in seconds (s).
	Spd             float32 `json:"spd"`   // Speed of the vehicle, in meters per second (m/s).
	Acc             float32 `json:"acc"`   // Acceleration (m/s^2), calculated from the speed on this and the previous message
	RouteID         string  `json:"route"` // ID of the route the vehicle is currently running on. Matches route_id in the topic.
	Stop            int     `json:"stop"`
	Occupancy       int     `json:"occu"` // Integer describing passenger occupancy level of the vehicle on [0, 100]
}

// GetEventHash  -
func (e *Event) GetEventHash() string {

	// Create a Hash of the Object's key Identifying Features
	h := md5.New()
	io.WriteString(h, fmt.Sprintf("%d:%s:%s", e.JrnID, e.RouteID, e.ODay))
	journeyID := fmt.Sprintf("%x", h.Sum(nil))

	log.Infof("%d:%s:%s", e.JrnID, e.RouteID, e.ODay)
	log.Infof(journeyID)

	return journeyID

}

// EventHolder is a struct used to capture the top-level of the MQTT
// message (MsgType) without extracting to rawJSON && reflecting.
//
// 4/24/2021: This struct is a placeholder for a struct
// containing all possible message types, VP is the most commmon....
type EventHolder struct {
	VP Event `json:"VP"`
}
