// Simple server implementation.

package main

import (
	"fmt"
	"html"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	connPort    = ":8080"
	requestFile = "request-file.txt"
	timeLapse   = time.Minute
)

// requestCounter contains a counter
// for the requests served.
type requestCounter struct {
	requestNo uint64
}

// Request counter file mutex.
var fileMutex sync.Mutex

// Request counter variable.
var requestCnt requestCounter

// Map containing the available resources.
var mux map[string]func(http.ResponseWriter, *http.Request)

// set sets the value of the request counter.
func (c *requestCounter) set(cnt uint64) {
	atomic.StoreUint64(&c.requestNo, cnt)
	requestCnt.updateFile()
}

// increment increments the value of the request counter.
func (c *requestCounter) increment() {
	atomic.AddUint64(&c.requestNo, 1)
	requestCnt.updateFile()
}

// reset resets the value of the request counter.
func (c *requestCounter) reset() {
	atomic.StoreUint64(&c.requestNo, 0)
	requestCnt.updateFile()
}

// value returns the value of the request counter.
func (c *requestCounter) value() (ret uint64) {
	ret = atomic.LoadUint64(&c.requestNo)
	return
}

// abort logs a fatal message during any
// operation related to the request count file.
func abort(action string, err error) {
	if err != nil {
		log.Fatalln("Could not", action,
			"the request count file:", err)
	}
}

// loadFile loads the number of requests
// from its relative file, if it exists.
func (c *requestCounter) loadFile() {
	if _, err := os.Stat(requestFile); err != nil {
		return
	}

	fileMutex.Lock()
	defer fileMutex.Unlock()
	reqNo, err := ioutil.ReadFile(requestFile)
	abort("read", err)
	requestNo, err := strconv.ParseUint(string(reqNo), 10, 64)
	abort("convert", err)

	atomic.StoreUint64(&c.requestNo, requestNo)
}

// updateFile writes the number
// of requests to a file.
func (c *requestCounter) updateFile() {
	fileMutex.Lock()
	defer fileMutex.Unlock()
	requestNo := strconv.FormatUint(c.value(), 10)

	err := ioutil.WriteFile(requestFile,
		[]byte(requestNo), 0644)
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

	requestCnt.increment()
	w.WriteHeader(http.StatusOK)
	tm := time.Now().Format(time.RFC1123)
	fmt.Fprintf(w, "Served %d requests in the last : %s\n",
		requestCnt.value(), time.Minute.String())
	fmt.Fprintf(w, "The time is: %s\n", tm)
}

// resetRequestNo runs every 'timeLapse'
// seconds to reset the request counter.
func resetRequestNo() {
	for {
		time.Sleep(timeLapse)
		requestCnt.reset()
		log.Println("Number of requests has been reset")
	}
}

// handleSigInt is handler for SIGINT.
func handleSingInt(c <-chan os.Signal) {
	for {
		sig := <-c
		if sig == os.Interrupt {
			requestCnt.updateFile()
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

	requestCnt.loadFile()
	go resetRequestNo()

	// Handle SIGINT
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go handleSingInt(c)

	log.Println("Listening...")
	log.Println("Type CTRL-C to stop the server")
	log.Fatalln("Server Close Error - ", server.ListenAndServe())
}
