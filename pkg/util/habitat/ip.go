package habitat

import (
	"fmt"
	"net"
)

// Todo: this can be pulled from cloud Metadata if inside VPC
func GetMyIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		fmt.Println("Error getting ip address")
		return ""
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func GetIPAddresses() []string {
	ifaddrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var addrs []string
	for _, addr := range ifaddrs {
		switch v := addr.(type) {
		case *net.IPNet:
			if v.IP.IsGlobalUnicast() {
				ip := v.IP
				addrs = append(addrs, ip.String())
			}
		}
	}
	return addrs
}

func GetPrimaryNetworkInterface() (string, error) {
	nics, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("Could not get interfaces: %v", err)
	}
	for _, nic := range nics {
		if nic.Flags&net.FlagUp != 1 ||
			nic.Flags&net.FlagLoopback != 0 ||
			nic.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, err := nic.Addrs()
		if err != nil {
			return "", fmt.Errorf("Getting IP addresses from %q: %v",
				nic.Name, err)
		}
		if addrs == nil {
			continue
		}
		for _, addr := range addrs {
			ip := net.ParseIP(addr.String())
			if ip.IsLoopback() ||
				ip.IsMulticast() ||
				ip.IsUnspecified() {
				continue
			}
			return nic.Name, nil
		}
	}
	return "", fmt.Errorf("No usable network interface found")
}
