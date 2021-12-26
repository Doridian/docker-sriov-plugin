package driver

import (
	"context"
	"fmt"

	"github.com/docker/docker/client"
)

func getRightClientApiVersion() (string, error) {
	// Start with the lowest API to query which version is supported.
	lowestCli, err3 := client.NewClientWithOpts(client.FromEnv, client.WithVersion("1.12"))
	if err3 != nil {
		fmt.Println("Fail to create client: ", err3)
		return "", err3
	}
	allVersions, err2 := lowestCli.ServerVersion(context.Background())
	if err2 != nil {
		fmt.Println("Error to get server version: ", err2)
		return "", err2
	}
	return allVersions.APIVersion, nil
}

func GetDockerAPIClient() (*client.Client, error) {
	var clientVersion string

	desiredVersion, err := getRightClientApiVersion()
	if err != nil {
		clientVersion = "unknown"
	} else {
		clientVersion = desiredVersion
	}
	cli, err2 := client.NewClientWithOpts(client.FromEnv, client.WithVersion(clientVersion))
	if err2 == nil {
		return cli, nil
	}
	return nil, err
}
