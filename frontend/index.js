import 'ol/ol.css';
import VectorTileLayer from 'ol/layer/VectorTile';
import VectorTileSource from 'ol/source/VectorTile';
import VectorLayer from 'ol/layer/Vector';
import TileLayer from 'ol/layer/Tile';
import XYZ from 'ol/source/XYZ';
import Map from 'ol/Map.js';
import MVT from 'ol/format/MVT';
import {Style, Stroke, Fill, Circle} from 'ol/style';
import View from 'ol/View.js';
import {Vector as VectorSource} from 'ol/source';
import Point from 'ol/geom/Point';
import {Feature} from 'ol';
import {transform} from 'ol/proj';
import Overlay from 'ol/Overlay';
import {ScaleLine, defaults as defaultControls} from 'ol/control';

// Array
// #d3f2a3,#97e196,#6cc08b,#4c9b82,#217a79,#105965,#074050
var colorArray = ['#1b0c41', '#4a0c6b', '#781c6d', '#a52c60', '#cf4446', '#ed6925', '#fb9b06', '#f7d13d']
var api_host = process.env.API_HOST;

// Mappings; map the raw labels to labels and statistics 
// representation functions for bus popup 
var statisticsmappings = {
  "nimi": {
    "func": function(e){ return e },
    "label": "Neighborhood"
  },
  "spd": {
    "label": "Current Speed (km/H)", 
    "func": function(s) { return (s * 3600 / 1000) }
  },
  "dl": {
    "label": "Average Delay ", 
    "func": function(dl) { 
      if (!dl){
        return null
      }
      return new Date(Math.abs(dl) * 1000).toISOString().substr(11, 8)
    }
  },
}

var stopmappings = {
  "stop_code": {
    "label": "Stop Code",
    "func": function(e){ return e }
  },
  "stop_name": {
    "label": "Name",
    "func": function(e){ return e }
  },
  "zone": {
    "label": "Zone",
    "func": function(e){ return e }
  }
}

var mappings = {
  "veh": {
    "label": "Vehicle ID",
    "func": function(e){ return e }
  },
  "route": {
    "label": "Route ID",
    "func": function(e){ return e }
  },
  "tsi": {
    "label": "Last Update (UTC)",
    "func": function(tsi) { 
        var d = new Date((tsi) * 1000).toISOString().substr(11, 8) 
        return d
     }
  },
  "spd": {
    "label": "Current Speed (km/H)", 
    "func": function(s) { return (s * 3600 / 1000) }
  },
  "stop": {
    "label": "Approaching Stop",
    "func": function(e){ return e }
  },
  "dl": {
    "label": "Behind Schedule", 
    "func":  function (dl) {
      if (dl > 0){
        return "On-Time" 
      }
      return new Date(Math.abs(dl) * 1000).toISOString().substr(11, 8)
    }
  }
}


var histmappings = {
  "veh": {
    "label": "Vehicle ID",
    "func": function(e){ return e }
  },
  "route": {
    "label": "Route ID",
    "func": function(e){ return e }
  },
  "tsi": {
    "label": "Last Update (UTC)",
    "func": function(tsi) { 
        var d = new Date((tsi)).toISOString().substr(11, 8) 
        return d
     }
  },
  "spd": {
    "label": "Speed (km/H)", 
    "func": function(s) { return (s * 3600 / 1000) }
  }
}


// Static Background Layer - Stops, Sourced from static GTFS feed data
var customStyleFunction = function(feature) {

  // Add Speed Here ...
  var p = feature.getProperties()
  var colorIndex = Math.round((p.spd/25) * colorArray.length)
  
  return [
    new Style({
      image: new Circle({
        radius: 4,
        fill: new Fill({ color: colorArray[colorIndex]}),
      }),
      stroke: new Stroke({
        color: 'rgb(0, 0, 0, 1)',
        width: 2
    })
    })
  ];
};



// Static Background Layer - Stops, Sourced from static GTFS feed data
var customStyleFunctionAreasSpeed = function(feature) {

  // Add Speed Here ...
  var p = feature.getProperties()
  var colorIndex = Math.round((p.spd/16) * colorArray.length)
  
  return [new Style({
    stroke: new Stroke({
      color: 'rgba(0, 0, 0, 1.0)',
      width: 1,
    }),
    fill: new Fill({
      color: colorArray[colorIndex],
    }),
  })]
};



function eventToTable(event, mappings) {
  // Given a Single JSON event -> Return the HTML to create
  // a 2 x N table of statistics -> values...
  let cols = Object.keys(event[0]);
  
  // For each key in the event, validate against the mappings
  // only return those w. a valid mapping...
  let rows = cols
    .map(c => {
      if (mappings[c]){
        // Return a single row from the keys from the event...
        return `<tr><td>${mappings[c]["label"]}</td><td>${mappings[c]["func"](event[0][c])}</td></tr>`;
      }
    })
    .join("");

  // concat the HTML table together
  const table = `
	<table>
		<thead>
			<tr><th>Attribute</th><th>Value</th></tr>
		<thead>
		<tbody>
			${rows}
		<tbody>
	<table>`;

  return table;
};

// Static Background Layer - Sourced From CARTO - Receive background *.png files
var cartoRasterLayer = new TileLayer({
  source: new XYZ({
    url: 'https://{a-d}.basemaps.cartocdn.com/rastertiles/light_all/{z}/{x}/{y}.png',
    attributions:'Map data &copy;<a href="https://www.openstreetmap.org/">OpenStreetMap</a> contributors, <a href="https://creativecommons.org/licenses/by-sa/2.0/">CC-BY-SA</a>',
  })
});

// Static Background Layer - Routes, Sourced from static GTFS feed data
var routesVectorLayer = new VectorTileLayer({
  source: new VectorTileSource({
    format: new MVT(),
    url: "https://" + api_host + "/tiles/routes/{z}/{x}/{y}",
    attributions: 'Schedule data &copy;<a href="https://transitfeeds.com/news/open-mobility-data">OpenMobilityData</a>'
  }),
  style: new Style({
    stroke: new Stroke({
        color: 'rgb(15, 15, 15, .4)',
        width: 2
    })
  })
})

routesVectorLayer.setVisible(false)

var stopsVectorLayer = new VectorTileLayer({
  source: new VectorTileSource({
    format: new MVT(),
    url: "https://" + api_host + "/tiles/stops/{z}/{x}/{y}",
    attributions: 'Transit data &copy; <a href="https://transitfeeds.com/news/open-mobility-data">OpenMobilityData</a>'
  }),
  style: new Style({
    image: new Circle({
      radius: 3,
      fill: new Fill({ color: '#151515'}),
    })
  })
})

stopsVectorLayer.setVisible(false)


var areasVectorLayer = new VectorTileLayer({
  source: new VectorTileSource({
    format: new MVT(),
    url: "https://" + api_host + "/tiles/statistics/{z}/{x}/{y}",
    attributions: 'Transit data &copy; <a href="https://transitfeeds.com/news/open-mobility-data">OpenMobilityData</a>'
  }),
  style: customStyleFunctionAreasSpeed
})

areasVectorLayer.setOpacity(0.3)
areasVectorLayer.setVisible(false)


// Live Foreground Layer - Vehicle Positions - Initializes with an empty feature list &&
// Reads from a socket connection and pushes incoming messages to the layer
var objSource = new VectorSource();

var livePositionsLayer = new VectorLayer({
  source: objSource,
  style: new Style({
    image: new Circle({
      radius: 3,
      fill: new Fill({ color: '#1557FF'}),
    }),
    stroke: new Stroke({
      color: 'rgb(0, 0, 0, 1)',
      width: 1
  })
  })
});

livePositionsLayer.setVisible(false)


// Hist
var objSourceHist = new VectorSource();

var histPositionsLayer = new VectorLayer({
  source: objSourceHist,
  style: customStyleFunction,
});


// Initialize the socket connection on page load 
//
// [WARNING]: No objects on Init, takes ~2s to properly populate after ws opening or 
// reopening
//
// MessageHandler for socket connection -> 
// When receive a new message -> push it to a list that holds data for live positions layer
function eventMsgHandler(event) {

    var obj = JSON.parse(event.data);
    
    // Create a UniqueID for each Bus, Train, etc based on Vehicle ID and route
    // some duplicate Vehicle IDs in fleet, not sure why, concat w. route resolves
    // this...
    var loc = objSource.getFeatureById([obj.VP.route, obj.VP.veh].join("/"));
    
    // If The point is already seen, then move the point to the new location...
    if (loc) {
      loc.getGeometry().setCoordinates(
        transform([obj.VP.long, obj.VP.lat], 'EPSG:4326', 'EPSG:3857')
      );

      loc.setProperties(obj)
      return
    }

    // Otherwise, update the features-set by adding a new position...
    var loc = new Feature({
      geometry: new Point(
        transform([obj.VP.long, obj.VP.lat], 'EPSG:4326', 'EPSG:3857')
        )
    });

    loc.setId([obj.VP.route, obj.VP.veh].join("/"))
    loc.setProperties(obj)

    objSource.addFeature(loc) 
}

// Using the sockets to source data onto the map gets expensive w. certain
// selections; toggle layer off also closes websocket s.t NO events are 
// processed until (re)connect

document.getElementById("live-toggle").addEventListener("click", function() {  
  
  var state = livePositionsLayer.getVisible()
  
  
  if (state){
    window.ws.close(); window.ws = null;
    livePositionsLayer.setVisible(false);
    return
  } 

  // Think this works...check w. on which browsers...
  livePositionsLayer.setVisible(true);
  window.ws = new WebSocket("wss://" + api_host + "/live/locations/");
  window.ws.onmessage = eventMsgHandler
});

document.getElementById("routes-toggle").addEventListener("click", function() {
  var state = routesVectorLayer.getVisible()
  routesVectorLayer.setVisible(!state);
});

document.getElementById("stops-toggle").addEventListener("click", function() {
  var state = stopsVectorLayer.getVisible()
  stopsVectorLayer.setVisible(!state);
});

document.getElementById("areas-stats-toggle").addEventListener("click", function() {
  var state = areasVectorLayer.getVisible()
  areasVectorLayer.setVisible(!state);
});


// Handling for Data Overlays

// Overlay/Pop-Up Layer handles for data that appears when objects are 
// clicked or hovered over
var container = document.getElementById('popup');
var content = document.getElementById('popup-content');

// Create an overlay to anchor the popup to the map; centers the highlighted element
var overlay = new Overlay({
  element: container,
  autoPan: true,
  autoPanAnimation: {
    duration: 150,
  },
});

// Define the Map Object and Apply Layers...
// Coords + Approx Bounds from: https://epsg.io/map#srs=3857
const map = new Map({
    target: 'map',
    controls: defaultControls().extend([new ScaleLine({
      units: 'metric',
      text: true,
      minWidth: 140,
    })]),
    layers: [
      cartoRasterLayer,
      livePositionsLayer,
      routesVectorLayer,
      stopsVectorLayer,
      areasVectorLayer,
      histPositionsLayer
    ],
    overlays: [overlay],
    view: new View({
      center: [2775954.001604, 8449262.50], 
      projection: 'EPSG:3857',
      zoom: 11,
      extent: [2550000, 830000, 2900000, 8700000],
    })
});

// Apply Map Level Handlers
map.on('pointermove', function(event) {
  // Handle for Highlighting 
  overlay.setPosition(undefined);

  map.forEachFeatureAtPixel(event.pixel, function(feature) {

    // getGeom & getProperty for each geom on hover && activate && set 
    // InnerHTML of pop-up
    var geometry = feature.getGeometry();
    var objProp = feature.getProperties();

    if (geometry) {
      
      // If the Object is a Vehicle...
      if (objProp["VP"]){
        content.innerHTML = eventToTable( [objProp["VP"]], mappings);
        overlay.setPosition(geometry.getCoordinates());
        return
      }
  
      // If the object is static (e.g. stop or route...)
      if (objProp["layer"] == 'stops'){
        content.innerHTML = eventToTable([objProp], stopmappings);
        overlay.setPosition(event.coordinate);
        return
      }

      if (objProp["layer"] == 'statistics'){
        content.innerHTML = eventToTable([objProp], statisticsmappings);
        overlay.setPosition(event.coordinate);
        return
      }

      if (objProp["layer"] == 'routes'){    
        // Hard skip
        return
      }

      // Otherwise - Share What Is Available; Historical Positions...
      content.innerHTML = eventToTable([objProp], histmappings);
      overlay.setPosition(event.coordinate);  
    }
  }, 
  {
    hitTolerance: 2
  });
});


document.getElementById("layerbar").addEventListener("click", function() {
    objSourceHist.clear()
});

map.on('click', function(event) {
    objSourceHist.clear()

    map.forEachFeatureAtPixel(event.pixel, function(feature, layer) {

      var objProp = feature.getProperties();

      if (objProp["VP"]){
          var data = {
            route: objProp["VP"].route, 
            jrn: objProp["VP"].jrn,
            oday: objProp["VP"].oday
          }

          fetch('https://' + api_host + '/live/histlocations/', {method: 'POST', body: JSON.stringify(data)}).then(function (response) {
            return response.json(); // The API call was successful!
          }).then(function (data) {
            // This is the JSON from our response
        
            for (const d of data) {
              var loc = new Feature({
                geometry: new Point(transform([d.long, d.lat], 'EPSG:4326', 'EPSG:3857'))
              });
              
              d.jrn = objProp["VP"].jrn
              d.route = objProp["VP"].route
              d.oday = objProp["VP"].oday
              d.veh = objProp["VP"].veh

              loc.setProperties(d)
              objSourceHist.addFeature(loc)
            }
        
          }).catch(function (err) {
            // There was an error
            console.warn('Something went wrong.', err);
          });
      }

    });
});