package main

import (
	"net"
	"strings"
)

// cgnat: 100.64.0.0/10, shared addresses (a provider's NAT, Tailscale): a machine of a private network.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// adresseDeploiement: host is the address of one of the projects' deployments, set by the owner.
func (s *Serveur) adresseDeploiement(host string) bool {
	if s.acces == nil || host == "" {
		return false
	}
	for _, p := range s.nomsProjets() {
		for _, e := range s.acces.Environnements(p) {
			if hoteURL(e.URL) == strings.ToLower(host) {
				return true
			}
		}
	}
	return false
}

// adressePublique: an address of the internet, never this PC, a LAN, a link, a VPN's shared range or a reserved one.
// A site's external check may reach it whatever stands in front of the server (a CDN or proxy such as Cloudflare,
// a reverse proxy on another machine, a load balancer); a private address stays limited to the servers' own.
func adressePublique(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !cgnat.Contains(ip)
}
