package driver

import (
	"context"
	"fmt"
	"log"
	"net"
	"reflect"
	"sync"

	"github.com/Mellanox/sriovnet"
	"github.com/docker/docker/api/types"
	"github.com/docker/go-plugins-helpers/network"
	"github.com/docker/libnetwork/netlabel"
	"github.com/docker/libnetwork/options"
)

const (
	containerVethPrefix = "eth"
	networkDevice       = "netdevice" // netdevice interface -o netdevice

	networkMode       = "mode"
	networkModePT     = "passthrough"
	networkModeSRIOV  = "sriov"
	sriovVlan         = "vlan"
	networkPrivileged = "privileged"
	ethPrefix         = "prefix"
	roceHopLimit      = "rocehoplimit"

	DriverName = "sriov"
)

type ptEndpoint struct {
	/* key */
	id string

	/* value */
	HardwareAddr string
	devName      string
	mtu          int
	Address      string
	sandboxKey   string
	vfName       string
	vfObj        *sriovnet.VfObj
}

type genericNetwork struct {
	id            string
	lock          sync.Mutex
	IPv4Data      *network.IPAMData
	ndevEndpoints map[string]*ptEndpoint
	driver        *driver // The network's driver
	mode          string  // SRIOV or Passthough
	ethPrefix     string

	ndevName string
}

type ptNetwork struct {
	genNw *genericNetwork
}

type NwIface interface {
	CreateNetwork(d *driver, genNw *genericNetwork,
		nid string, options map[string]string,
		ipv4Data *network.IPAMData) error
	DeleteNetwork(d *driver, req *network.DeleteNetworkRequest)

	CreateEndpoint(r *network.CreateEndpointRequest) (*network.CreateEndpointResponse, error)
	DeleteEndpoint(endpoint *ptEndpoint)

	getGenNw() *genericNetwork
}

type driver struct {
	// below map maps a network id to NwInterface object
	networks        map[string]NwIface
	networksFetched bool
	sync.Mutex
}

func createGenNw(nid string, ndevName string,
	networkMode string, ethPrefix string, ipv4Data *network.IPAMData) *genericNetwork {

	genNw := genericNetwork{}
	ndevs := map[string]*ptEndpoint{}
	genNw.id = nid
	genNw.mode = networkMode
	genNw.IPv4Data = ipv4Data
	genNw.ndevEndpoints = ndevs
	genNw.ndevName = ndevName
	genNw.ethPrefix = ethPrefix

	return &genNw
}

func (d *driver) GetCapabilities() (*network.CapabilitiesResponse, error) {
	return &network.CapabilitiesResponse{Scope: network.LocalScope}, nil
}

// parseNetworkGenericOptions parses generic driver docker network options
func parseNetworkGenericOptions(data interface{}) (map[string]string, error) {
	var err error

	options := make(map[string]string)

	switch opt := data.(type) {
	case map[string]interface{}:
		for key, value := range opt {
			options[key] = fmt.Sprintf("%s", value)
		}
		log.Printf("parseNetworkGenericOptions %v\n", options)
	case map[string]string:
		options = opt
	default:
		log.Printf("unrecognized network config format: %v\n", reflect.TypeOf(opt))
	}

	if options[networkMode] == "" {
		// default to sriov
		options[networkMode] = networkModeSRIOV
	} else {
		if options[networkMode] != networkModePT &&
			options[networkMode] != networkModeSRIOV {
			return options, fmt.Errorf("valid modes are: passthrough and sriov")
		}
	}
	if options[networkDevice] == "" {
		if options[networkMode] == networkModeSRIOV {
			return options, fmt.Errorf("sriov mode requires netdevice")
		} else {
			return options, fmt.Errorf("passthrough mode requires netdevice")
		}
	}

	if options[ethPrefix] == "" {
		options[ethPrefix] = containerVethPrefix
	}

	return options, err
}

func parseNetworkOptions(id string, option options.Generic) (map[string]string, error) {
	// parse generic labels first
	genData, ok := option[netlabel.GenericData]
	if ok && genData != nil {
		options, err := parseNetworkGenericOptions(genData)

		return options, err
	}
	return nil, fmt.Errorf("invalid options")
}

func (d *driver) createNetwork(nid string, options map[string]string,
	ipv4Data *network.IPAMData) (NwIface, error) {
	var err error

	genNw := createGenNw(nid, options[networkDevice], options[networkMode], options[ethPrefix], ipv4Data)

	var nw NwIface
	if options[networkMode] == "passthrough" {
		nw = &ptNetwork{}
	} else {
		var multiport bool

		multiport = checkMultiPortDevice(options[networkDevice])
		if multiport == true {
			log.Println("Multiport driver for device: ", options[networkDevice])
			nw = &dpSriovNetwork{}
		} else {
			log.Println("Single port driver for device: ", options[networkDevice])
			nw = &sriovNetwork{}
		}
	}

	err = nw.CreateNetwork(d, genNw, nid, options, ipv4Data)
	if err != nil {
		return nil, err
	}
	return nw, nil
}

func (d *driver) registerNetwork(nid string, options map[string]string,
	ipv4Data *network.IPAMData) error {

	net, err := d.createNetwork(nid, options, ipv4Data)
	if err == nil {
		d.networks[nid] = net
	}
	return err
}

func (d *driver) ensureNetworksFetched() {
	d.Lock()
	defer d.Unlock()

	if d.networksFetched {
		return
	}

	cli, err := GetDockerAPIClient()
	if err != nil {
		log.Printf("ensureNetworksFetched(): GetAPIClient %v\n", err)
		return
	}
	networks, err := cli.NetworkList(context.Background(), types.NetworkListOptions{})
	if err != nil {
		log.Printf("ensureNetworksFetched(): List %v\n", err)
		return
	}

	for _, net := range networks {
		if net.Driver != DriverName {
			continue
		}
		ipv4Data := network.IPAMData{}
		ipv4Data.Gateway = net.IPAM.Config[0].Gateway
		options, errp := parseNetworkGenericOptions(net.Options)
		if errp != nil {
			log.Printf("ensureNetworksFetched(): Parse %v\n", errp)
			continue
		}
		errp = d.registerNetwork(net.ID, options, &ipv4Data)
		if errp != nil {
			log.Printf("ensureNetworksFetched(): Register %v\n", errp)
			continue
		}
	}

	d.networksFetched = true
}

func (d *driver) CreateNetwork(req *network.CreateNetworkRequest) error {
	d.ensureNetworksFetched()

	log.Printf("CreateNetwork() : [ %+v ]\n", req)
	log.Printf("CreateNetwork IPv4Data len : [ %v ]\n", len(req.IPv4Data))

	d.Lock()
	defer d.Unlock()

	if req.IPv4Data == nil || len(req.IPv4Data) == 0 {
		return fmt.Errorf("Network gateway config miss.")
	}

	options, ret := parseNetworkOptions(req.NetworkID, req.Options)
	if ret != nil {
		log.Printf("CreateNetwork network options parse error")
		return ret
	}

	ipv4Data := req.IPv4Data[0]

	return d.registerNetwork(req.NetworkID, options, ipv4Data)
}

func (d *driver) AllocateNetwork(r *network.AllocateNetworkRequest) (*network.AllocateNetworkResponse, error) {
	log.Printf("AllocateNetwork() [ %+v ]\n", r)
	return nil, nil
}

func (d *driver) DeleteNetwork(req *network.DeleteNetworkRequest) error {
	d.ensureNetworksFetched()

	log.Printf("DeleteNetwork() [ %+v ]\n", req)

	d.Lock()
	defer d.Unlock()

	nw := d.networks[req.NetworkID]
	if nw != nil {
		nw.DeleteNetwork(d, req)
	}

	delete(d.networks, req.NetworkID)

	return nil
}

func (d *driver) FreeNetwork(r *network.FreeNetworkRequest) error {
	log.Printf("FreeNetwork() [ %+v ]\n", r)
	return nil
}

func StartDriver() (*driver, error) {

	// allocate an empty map of network objects that can
	// be later on referred by using id passed in CreateNetwork, DeleteNetwork
	// etc operations.

	dnetworks := make(map[string]NwIface)

	driver := &driver{
		networks: dnetworks,
	}

	return driver, nil
}

func (d *driver) CreateEndpoint(r *network.CreateEndpointRequest) (*network.CreateEndpointResponse, error) {
	d.ensureNetworksFetched()
	d.Lock()
	defer d.Unlock()

	log.Printf("CreateEndpoint() [ %+v ]\n", r)
	log.Printf("r.Interface: [ %+v ]\n", r.Interface)

	nw := d.networks[r.NetworkID]
	if nw == nil {
		return nil, fmt.Errorf("Plugin can not find network [ %s ].", r.NetworkID)
	}

	return nw.CreateEndpoint(r)
}

func getEndpoint(genNw *genericNetwork, endpointID string) *ptEndpoint {
	return genNw.ndevEndpoints[endpointID]
}

func (nw *ptNetwork) getGenNw() *genericNetwork {
	return nw.genNw
}

func (d *driver) getGenNwFromNetworkID(networkID string) *genericNetwork {
	d.ensureNetworksFetched()
	nw := d.networks[networkID]
	if nw == nil {
		return nil
	}
	return nw.getGenNw()
}

func (d *driver) EndpointInfo(r *network.InfoRequest) (*network.InfoResponse, error) {
	log.Printf("EndpointInfo: [ %+v ]\n", r)
	d.Lock()
	defer d.Unlock()

	genNw := d.getGenNwFromNetworkID(r.NetworkID)
	if genNw == nil {
		return nil, fmt.Errorf("Can not find network [ %s ].", r.NetworkID)
	}

	endpoint := getEndpoint(genNw, r.EndpointID)
	if endpoint == nil {
		return nil, fmt.Errorf("Cannot find endpoint by id: %s", r.EndpointID)
	}

	value := make(map[string]string)
	value["id"] = endpoint.id
	value["srcName"] = endpoint.devName
	resp := &network.InfoResponse{
		Value: value,
	}
	log.Printf("EndpointInfo resp.Value : [ %+v ]\n", resp.Value)
	return resp, nil
}

func (d *driver) Join(r *network.JoinRequest) (*network.JoinResponse, error) {
	log.Printf("Join() [ %+v ]\n", r)

	d.Lock()
	defer d.Unlock()

	genNw := d.getGenNwFromNetworkID(r.NetworkID)
	if genNw == nil {
		return nil, fmt.Errorf("Can not find network [ %s ].", r.NetworkID)
	}

	endpoint := getEndpoint(genNw, r.EndpointID)
	if endpoint == nil {
		return nil, fmt.Errorf("Cannot find endpoint by id: %s", r.EndpointID)
	}

	if endpoint.sandboxKey != "" {
		return nil, fmt.Errorf("Endpoint [%s] has bean bind to sandbox [%s]", r.EndpointID, endpoint.sandboxKey)
	}
	gw, _, err := net.ParseCIDR(genNw.IPv4Data.Gateway)
	if err != nil {
		return nil, fmt.Errorf("Parse gateway [%s] error: %s", genNw.IPv4Data.Gateway, err.Error())
	}
	endpoint.sandboxKey = r.SandboxKey
	resp := network.JoinResponse{
		InterfaceName: network.InterfaceName{
			SrcName:   endpoint.devName,
			DstPrefix: genNw.ethPrefix,
		},
		DisableGatewayService: false,
		Gateway:               gw.String(),
	}

	log.Printf("Join resp : [ %+v ]\n", resp)
	return &resp, nil
}

func (d *driver) Leave(r *network.LeaveRequest) error {
	log.Printf("Leave(): [ %+v ]\n", r)
	d.Lock()
	defer d.Unlock()

	genNw := d.getGenNwFromNetworkID(r.NetworkID)
	if genNw == nil {
		return fmt.Errorf("Can not find network [ %s ].", r.NetworkID)
	}

	endpoint := getEndpoint(genNw, r.EndpointID)
	if endpoint == nil {
		return fmt.Errorf("Cannot find endpoint by id: %s", r.EndpointID)
	}

	endpoint.sandboxKey = ""
	return nil
}

func (d *driver) DeleteEndpoint(r *network.DeleteEndpointRequest) error {
	d.ensureNetworksFetched()
	log.Printf("DeleteEndpoint() [ %+v ]\n", r)

	d.Lock()
	defer d.Unlock()

	genNw := d.getGenNwFromNetworkID(r.NetworkID)
	if genNw == nil {
		return fmt.Errorf("Can not find network [ %s ].", r.NetworkID)
	}

	endpoint := getEndpoint(genNw, r.EndpointID)
	if endpoint == nil {
		return fmt.Errorf("Cannot find endpoint by id: %s", r.EndpointID)
	}

	nw := d.networks[r.NetworkID]

	nw.DeleteEndpoint(endpoint)
	delete(genNw.ndevEndpoints, r.EndpointID)
	return nil
}

func (d *driver) DiscoverNew(r *network.DiscoveryNotification) error {
	log.Printf("DiscoverNew(): [ %+v ]\n", r)
	return nil
}

func (d *driver) DiscoverDelete(r *network.DiscoveryNotification) error {
	log.Printf("DiscoverDelete: [ %+v ]\n", r)
	return nil
}

func (d *driver) ProgramExternalConnectivity(r *network.ProgramExternalConnectivityRequest) error {
	log.Printf("ProgramExternalConnectivity(): [ %+v ]\n", r)
	return nil
}

func (d *driver) RevokeExternalConnectivity(r *network.RevokeExternalConnectivityRequest) error {
	log.Printf("RevokeExternalConnectivity(): [ %+v ]\n", r)
	return nil
}

func (pt *ptNetwork) CreateNetwork(d *driver, genNw *genericNetwork,
	nid string, options map[string]string,
	ipv4Data *network.IPAMData) error {

	pt.genNw = genNw

	log.Printf("PT CreateNetwork : [%s] IPv4Data : [ %+v ]\n", pt.genNw.id, pt.genNw.IPv4Data)
	return nil
}

func (pt *ptNetwork) DeleteNetwork(d *driver, req *network.DeleteNetworkRequest) {

}

func (nw *ptNetwork) CreateEndpoint(r *network.CreateEndpointRequest) (*network.CreateEndpointResponse, error) {
	if len(nw.genNw.ndevEndpoints) > 0 {
		return nil, fmt.Errorf("supports only one device")
	}

	ndev := &ptEndpoint{
		devName: nw.genNw.ndevName,
		Address: r.Interface.Address,
	}
	nw.genNw.ndevEndpoints[r.EndpointID] = ndev

	endpointInterface := &network.EndpointInterface{}
	if r.Interface.Address == "" {
		endpointInterface.Address = ndev.Address
	}
	if r.Interface.MacAddress == "" {
		//endpointInterface.MacAddress = ndev.HardwareAddr
	}
	resp := &network.CreateEndpointResponse{Interface: endpointInterface}
	log.Printf("PT CreateEndpoint resp interface: [ %+v ] ", resp.Interface)
	return resp, nil
}

func (nw *ptNetwork) DeleteEndpoint(endpoint *ptEndpoint) {

}
