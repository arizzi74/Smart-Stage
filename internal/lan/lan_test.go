package lan

import (
	"errors"
	"net"
	"reflect"
	"testing"
)

func TestDiscoveryOnANetworkWithoutDHCP(t *testing.T) {
	interfaces := []net.Interface{
		{Name: "unavailable", Flags: net.FlagUp},
		{Name: "wifi", Flags: net.FlagUp},
		{Name: "down"},
		{Name: "loopback", Flags: net.FlagUp | net.FlagLoopback},
		{Name: "ethernet", Flags: net.FlagUp},
	}
	got := collectAddresses(interfaces, func(iface *net.Interface) ([]net.Addr, error) {
		var cidrs []string
		switch iface.Name {
		case "unavailable":
			return nil, errors.New("interface removed during enumeration")
		case "down", "loopback":
			t.Fatalf("inactive interface %q was queried", iface.Name)
		case "wifi":
			cidrs = []string{"192.168.5.20/24", "2001:db8::20/64", "fe80::20/64"}
		case "ethernet":
			cidrs = []string{"169.254.10.20/16", "127.0.0.1/8", "0.0.0.0/32", "224.0.0.1/32"}
		}
		var out []net.Addr
		for _, cidr := range cidrs {
			ip, network, err := net.ParseCIDR(cidr)
			if err != nil {
				t.Fatal(err)
			}
			network.IP = ip
			out = append(out, network)
		}
		return out, nil
	})
	want := []Address{{"ethernet", "169.254.10.20"}, {"wifi", "192.168.5.20"}, {"wifi", "2001:db8::20"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LAN candidates = %#v; want %#v", got, want)
	}
	if url := URL(got[0].IP, 8787, "/command"); url != "http://169.254.10.20:8787/command" {
		t.Fatal(url)
	}
	if url := URL(got[2].IP, 8787, "/admin"); url != "http://[2001:db8::20]:8787/admin" {
		t.Fatal(url)
	}
	if hosts := Hosts(got, "0.0.0.0"); !reflect.DeepEqual(hosts, []string{"127.0.0.1", "::1", "localhost", "169.254.10.20", "192.168.5.20"}) {
		t.Fatalf("IPv4 listener hosts = %v", hosts)
	}
	if hosts := Hosts(got, "169.254.10.20"); !reflect.DeepEqual(hosts, []string{"169.254.10.20"}) {
		t.Fatalf("interface-restricted hosts = %v", hosts)
	}
}
