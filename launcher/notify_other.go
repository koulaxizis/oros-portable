//go:build !windows

package main

import (
	"log"
	"os"
)

func notify(msg string) { log.Print(msg) }

func fail(msg string) {
	log.Print(msg)
	os.Exit(1)
}
