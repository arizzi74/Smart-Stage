package main

import (
	"strings"
	"testing"

	"smartstage/internal/lan"
)

func TestRemoteLinksMatchListenerAndPreferredInterface(t *testing.T) {
	addresses := []lan.Address{{Interface: "en0", IP: "192.168.1.7"}, {Interface: "en1", IP: "10.0.0.7"}, {Interface: "en0", IP: "fd00::7"}}
	links := remoteLinks(addresses, "0.0.0.0", "10.0.0.7", 8788, "01234567")
	if len(links) != 2 || links[0].URL != "http://10.0.0.7:8788/command#token=01234567" || !strings.Contains(links[0].Label, "preferred") {
		t.Fatalf("wrong advertised links: %+v", links)
	}
	links = remoteLinks(addresses, "fd00::7", "", 8989, "01234567")
	if len(links) != 1 || links[0].URL != "http://[fd00::7]:8989/command#token=01234567" {
		t.Fatalf("wrong IPv6 link: %+v", links)
	}
	if links := remoteLinks(addresses, "127.0.0.1", "", 8788, "01234567"); len(links) != 0 {
		t.Fatalf("advertised unreachable LAN links: %+v", links)
	}
}
