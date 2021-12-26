package driver

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/Mellanox/rdmamap"
)

func setRoceHopLimitWA(netdevice string, hopLimit uint8) error {
	rdmadev, err := rdmamap.GetRdmaDeviceForNetdevice(netdevice)
	if err != nil {
		return err
	}

	file := filepath.Join(rdmamap.RdmaClassDir, rdmadev, "ttl", "1", "ttl")

	ttlFile, err := os.OpenFile(file, os.O_WRONLY, 0444)
	if err != nil {
		return err
	}
	defer ttlFile.Close()
	_, err = ttlFile.WriteString(strconv.Itoa(int(hopLimit)))
	return err
}
