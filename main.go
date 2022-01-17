package main

import (
	"log"
	"os"

	"github.com/docker/go-plugins-helpers/network"

	"github.com/FoxDenHome/docker-sriov-plugin/driver"
)

var version = "DEV"

func main() {
	d, err := driver.StartDriver()
	if err != nil {
		panic(err)
	}
	h := network.NewHandler(d)

	log.Printf("Docker sriov plugin started version=%v\n", version)
	log.Printf("Ready to accept commands.\n")

	err = h.ServeUnix("sriov", 0)
	if err != nil {
		log.Fatalf("Run app error: %s", err.Error())
		os.Exit(1)
	}
}
