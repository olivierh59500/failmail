package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

type TerminationRequest int

const (
	GracefulShutdown TerminationRequest = iota
	Reload
)

// Listens for a SIGTERM or SIGUSR1, forwards it on as a `TerminationRequest`
// to all subscribers, and returns true if a reload is required.
func HandleSignals(reqs []chan<- TerminationRequest) bool {
	signals := make(chan os.Signal, 0)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1)
	sig := <-signals
	log.Printf("caught signal %s", sig)
	for _, req := range reqs {
		if sig == syscall.SIGUSR1 {
			req <- Reload
		} else {
			req <- GracefulShutdown
		}
	}
	return sig == syscall.SIGUSR1
}
