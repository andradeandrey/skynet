//Copyright (c) 2011 Brian Ketelsen

//Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

//The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

//THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package skylib

import (
	"log"
	"json"
	"flag"
	"os"
	"fmt"
	"rand"
	"rpc"
	"expvar"
)


var NS *NetworkServers
var RpcServices []*RpcService


var Port *int = flag.Int("port", 9999, "tcp port to listen")
var Name *string = flag.String("name", os.Args[0], "name of this server")
var BindIP *string = flag.String("bindaddress", "127.0.0.1", "address to bind")
var LogFileName *string = flag.String("logFileName", "myservice.log", "name of logfile")
var LogLevel *int = flag.Int("logLevel", 1, "log level (1-5)")
var Protocol *string = flag.String("protocol", "http+gob", "RPC message transport protocol (default is http+gob; try json")
var Requests *expvar.Int
var Errors *expvar.Int
var Goroutines *expvar.Int
var svc *Service


func GetServiceProviders(provides string) (providesList []*Service) {
	for _, v := range NS.Services {
		if v != nil && v.Provides == provides {
			providesList = append(providesList, v)
		}
	}
	return
}

// This is simple today - it returns the first listed service that matches the request
// Load balancing needs to be applied here somewhere.
func GetRandomClientByProvides(provides string) (*rpc.Client, os.Error) {
	var newClient *rpc.Client
	var err os.Error
	serviceList := GetServiceProviders(provides)

	if len(serviceList) > 0 {
		chosen := rand.Int() % len(serviceList)
		s := serviceList[chosen]

		hostString := fmt.Sprintf("%s:%d", s.IPAddress, s.Port)
		newClient, err = rpc.DialHTTP("tcp", hostString)
		if err != nil {
			LogWarn(fmt.Sprintf("Found %d Services to service %s request on %s.",
				len(serviceList), provides, hostString))
			return nil, NewError(NO_CLIENT_PROVIDES_SERVICE, provides)
		}

	} else {
		LogWarn(fmt.Sprintf("Found no Service to service %s request.", provides))
		return nil, NewError(NO_CLIENT_PROVIDES_SERVICE, provides)
	}
	return newClient, nil
}


// on startup load the configuration file. 
// After the config file is loaded, we set the global config file variable to the
// unmarshaled data, making it useable for all other processes in this app.
func LoadConfig() {
	data, _, err := DC.Get("/servers/config/networkservers.conf", nil)
	if err != nil {
		log.Panic(err.String())
	}
	if len(data) > 0 {
		setConfig(data)
		return
	}
	LogError("Error loading default config - no data found")
	NS = &NetworkServers{}
}

func RemoveServiceAt(i int) {

	newServices := make([]*Service, 0)

	for k, v := range NS.Services {
		if k != i {
			if v != nil {
				newServices = append(newServices, v)
			}
		}
	}
	NS.Services = newServices
	b, err := json.Marshal(NS)
	if err != nil {
		log.Panic(err.String())
	}
	rev, err := DC.Rev()
	if err != nil {
		log.Panic(err.String())
	}
	_, err = DC.Set("/servers/config/networkservers.conf", rev, b)
	if err != nil {
		log.Panic(err.String())
	}

}

func RemoveFromConfig(r *Service) {

	newServices := make([]*Service, 0)

	for _, v := range NS.Services {
		if v != nil {
			if !v.Equal(r) {
				newServices = append(newServices, v)
			}

		}
	}
	NS.Services = newServices
	b, err := json.Marshal(NS)
	if err != nil {
		log.Panic(err.String())
	}
	rev, err := DC.Rev()
	if err != nil {
		log.Panic(err.String())
	}
	_, err = DC.Set("/servers/config/networkservers.conf", rev, b)
	if err != nil {
		log.Panic(err.String())
	}
}

func AddToConfig(r *Service) {
	for _, v := range NS.Services {
		if v != nil {
			if v.Equal(r) {
				LogInfo(fmt.Sprintf("Skipping adding %s : alreday exists.", v.Name))
				return // it's there so we don't need an update
			}
		}
	}
	NS.Services = append(NS.Services, r)
	b, err := json.Marshal(NS)
	if err != nil {
		log.Panic(err.String())
	}
	rev, err := DC.Rev()
	if err != nil {
		log.Panic(err.String())
	}
	_, err = DC.Set("/servers/config/networkservers.conf", rev, b)
	if err != nil {
		log.Panic(err.String())
	}
}

// unmarshal data from remote store into global config variable
func setConfig(data []byte) {
	err := json.Unmarshal(data, &NS)
	if err != nil {
		log.Panic(err.String())
	}
}

// Watch for remote changes to the config file.  When new changes occur
// reload our copy of the config file.
// Meant to be run as a goroutine continuously.
func WatchConfig() {
	rev, err := DC.Rev()
	if err != nil {
		log.Panic(err.String())
	}

	for {
		// blocking wait call returns on a change
		ev, err := DC.Wait("/servers/config/networkservers.conf", rev)
		if err != nil {
			log.Panic(err.String())
		}
		log.Println("Received new configuration.  Setting local config.")
		setConfig(ev.Body)

		rev = ev.Rev + 1
	}

}


// Method to register the heartbeat of each skynet
// client with the healthcheck exporter.
func RegisterHeartbeat() {
	r := NewService("Service.Ping")
	rpc.Register(r)
}


//Connects to the global config repo and registers the
//name Skynet Service. This function is also responsible for
//registering the Heartbeat to healthcheck the service.
func Setup(name string) {
	DoozerConnect()
	LoadConfig()
	if x := recover(); x != nil {
		LogWarn("No Configuration File loaded.  Creating One.")
	}

	go watchSignals()

	initDefaultExpVars(name)

	svc = NewService(name)

	AddToConfig(svc)

	go WatchConfig()

	RegisterHeartbeat()

}
