// Example showing how to log HTTP requests
package main

import (
	"context"
	"io/ioutil"
	"log"
	"net/http/httptrace"
	"os"
	"strings"

	"github.com/ansd/lastpass-go"
)

func main() {
	// read LastPass username and master password from file
	b, err := ioutil.ReadFile("../credentials.txt")
	if err != nil {
		log.Fatalln(err)
	}
	lines := strings.Split(string(b), "\n")
	username := lines[0]
	masterPassword := lines[1]

	// There are three different options how to log HTTP requests.

	// option 1: enable logging for all methods on lastpass.Client
	// use any logger which implements lastpass.Logger (i.e. func Printf(format string, v ...interface{}))
	logger := log.New(os.Stderr, "client logger ", log.LstdFlags)
	client, err := lastpass.NewClient(context.Background(), username, masterPassword, lastpass.WithLogger(logger))
	if err != nil {
		log.Fatalln(err)
	}

	// option 2: enable logging for only a specific method (request scope)
	logger = log.New(os.Stderr, "context logger ", log.LstdFlags)
	_, err = client.Accounts(lastpass.NewContextWithLogger(context.Background(), logger))
	if err != nil {
		log.Fatalln(err)
	}

	// option 3: enable HTTP tracing for a specific method (request scope)
	logger = log.New(os.Stderr, "HTTP tracer ", log.LstdFlags)
	trace := &httptrace.ClientTrace{
		WroteHeaderField: func(key string, value []string) {
			if key == ":method" || key == ":path" {
				logger.Println(key, value)
			}
		},
	}
	if err = client.Logout(httptrace.WithClientTrace(context.Background(), trace)); err != nil {
		log.Fatalln(err)
	}
}
