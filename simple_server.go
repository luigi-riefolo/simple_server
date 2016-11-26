// Simple server implementation.

package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"
)

const (
	connPort    = ":8080"
	requestFile = "request-file.txt"
	// Time period for each
	// counter request increment.
	timeLapse = time.Second
	// Request counter time window.
	timeWindow = time.Minute
)

// requestCounter keeps track of the number of
// requests received in a 'timeWindow' period.
type requestCounter struct {
	// Mutex for set of consecutive operations
	// on the request counter variables.
	cntMutex sync.Mutex
	// Number of requests received
	// in a 'timeLapse' period.
	timeLapseReqNo uint64
	// Total number of requests received
	// in a 'timeWindow' period.
	timeWindowReqNo uint64
	// Delta's index for 'deltas'.
	deltaIdx uint
	// Stores all the 'timeLapseReqNo'
	// collected in a 'timeWindow' period.
	deltas []uint64
}

// Request counter variable.
var reqCnt requestCounter

// Map containing the available resources.
var mux map[string]func(http.ResponseWriter, *http.Request)

// Initialiase the data request counter structure.
func (c *requestCounter) init() {
	c.deltas = make([]uint64, 60)
	c.loadFile()
}

// increment increments the amount of requests
// received in a 'timeLapse' period.
func (c *requestCounter) increment() {
	c.cntMutex.Lock()
	c.timeLapseReqNo++
	c.cntMutex.Unlock()
}

// abort logs a fatal message during any
// operation related to the request count file.
func abort(action string, err error) {
	if err != nil {
		log.Fatalln("Could not", action,
			"the request count file:", err)
	}
}

// jsonData is a container for JSON data that have to
// be written or read to or from the request data file.
type jsonData struct {
	DeltaIdx        uint
	Deltas          []uint64
	TimeWindowReqNo uint64
}

// loadFile loads the number of requests
// from its relative file, if it exists.
func (c *requestCounter) loadFile() {
	if _, err := os.Stat(requestFile); err != nil {
		return
	}

	c.cntMutex.Lock()
	defer c.cntMutex.Unlock()

	data, err := os.Open(requestFile)
	abort("open", err)

	jsonData := jsonData{}
	err = json.NewDecoder(data).Decode(&jsonData)
	abort("JSON decode", err)

	// Load the JSON data
	c.deltaIdx = jsonData.DeltaIdx
	copy(c.deltas, jsonData.Deltas)
	atomic.StoreUint64(&c.timeWindowReqNo, jsonData.TimeWindowReqNo)
}

// updateFile writes the number
// of requests to a file.
func (c *requestCounter) updateFile() {
	c.cntMutex.Lock()
	defer c.cntMutex.Unlock()

	// Marshal the reqCnt into a JSON string
	jsonData := jsonData{
		c.deltaIdx,
		c.deltas,
		c.timeWindowReqNo,
	}

	data, err := json.Marshal(jsonData)
	abort("JSON encode", err)

	err = ioutil.WriteFile(requestFile, data, 0644)
	abort("update", err)
}

// handlerDispatcher routes a handler
// request to its relative worker function.
type handlerDispatcher struct{}

func (*handlerDispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Call the registered handler, it it exists
	if h, ok := mux[r.URL.String()]; ok {
		h(w, r)
		return
	}

	// Default to 404 for non existing resources
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, "Requested resource '%s' does not exist\n",
		html.EscapeString(r.URL.Path))
}

// printRequestNo is the worker
// function for the main hanlder.
func printRequestNo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	reqCnt.increment()
	w.WriteHeader(http.StatusOK)

	reqCnt.cntMutex.Lock()
	fmt.Fprintf(w, "Served %d requests in the last : %s\n",
		reqCnt.timeWindowReqNo+reqCnt.timeLapseReqNo, timeWindow)
	reqCnt.cntMutex.Unlock()

	tm := time.Now().Format(time.RFC1123)
	fmt.Fprintf(w, "The time is: %s\n", tm)
}

// updateTimeWindow updates and then reset the number
// of requests received in a 'timeLapse' period,
func (c *requestCounter) updateTimeWindow() {
	// Using a mutex avoids race conditions
	// that might arise getting and setting
	// the request counter's values.
	c.cntMutex.Lock()

	if float64(c.deltaIdx) == timeWindow.Seconds() {
		c.deltaIdx = 0
	}

	// Total requests = (total requests - previous delta) + new delta
	c.timeWindowReqNo -= c.deltas[c.deltaIdx]
	c.timeWindowReqNo += c.timeLapseReqNo

	// Store the new delta
	c.deltas[c.deltaIdx] = c.timeLapseReqNo
	c.timeLapseReqNo = 0

	c.deltaIdx++
	c.cntMutex.Unlock()
}

// updateTimeWindow runs every 'timeLapse'
// seconds and stores the amount of request
// received in 'timeLapse' seconds.
func updateRequestCounter() {
	for {
		time.Sleep(timeLapse)
		reqCnt.updateTimeWindow()
		reqCnt.updateFile()
	}
}

// handleSigInt is a handler for SIGINT.
func handleSingInt(c <-chan os.Signal) {
	for {
		// TODO:
		// Check that 'updateRequestCounter' has been run
		// for the current second, otherwise we loose the
		// current number of new requests.
		sig := <-c
		if sig == os.Interrupt {
			reqCnt.updateFile()
			log.Println("Stopping server...")
			// NOTE:
			// The effective exit code is 1,
			// instead of 0, this is due to
			// a bug in Go signal handling.
			os.Exit(0)
		}
	}
}

func main() {
	log.Println("Launching server")
	server := http.Server{
		Addr:           connPort,
		Handler:        &handlerDispatcher{},
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	// List of request handlers
	mux = make(map[string]func(http.ResponseWriter, *http.Request))
	mux["/"] = printRequestNo

	reqCnt.init()
	go updateRequestCounter()

	// Handle SIGINT
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go handleSingInt(c)

	log.Println("Listening...")
	log.Println("Type CTRL-C to stop the server")
	log.Fatalln("Server Close Error - ", server.ListenAndServe())
}
