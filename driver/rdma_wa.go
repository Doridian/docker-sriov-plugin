package driver

import (
	"path/filepath"

	"github.com/Mellanox/rdmamap"
)

func setRoceHopLimitWA(netdevice string, hopLimit uint8) error {
	rdmadev, err := rdmamap.GetRdmaDeviceForNetdevice(netdevice)
	if err != nil {
		return err
	}

	file := filepath.Join(rdmamap.RdmaClassDir, rdmadev, "ttl", "1", "ttl")

	ttlFile := fileObject{
		Path: file,
	}

	return ttlFile.WriteInt(int(hopLimit))
}
