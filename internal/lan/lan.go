package lan

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

type Address struct {
	Interface string
	IP        string
}

func Addresses() ([]Address, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	result := []Address{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
				continue
			}
			result = append(result, Address{iface.Name, ip.String()})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Interface != result[j].Interface {
			return result[i].Interface < result[j].Interface
		}
		return result[i].IP < result[j].IP
	})
	return result, nil
}
func URL(ip string, port int, path string) string {
	return "http://" + net.JoinHostPort(ip, fmt.Sprint(port)) + path
}
func Hosts(addresses []Address, bind string) []string {
	if bind != "0.0.0.0" && bind != "::" && bind != "" {
		return []string{bind}
	}
	hosts := []string{"127.0.0.1", "::1", "localhost"}
	for _, a := range addresses {
		if bind == "0.0.0.0" && strings.Contains(a.IP, ":") {
			continue
		}
		hosts = append(hosts, a.IP)
	}
	return hosts
}
