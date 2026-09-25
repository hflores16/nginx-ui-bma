package bma_metrics

import (
	"context"
	"net"
	neturl "net/url"
	"sort"
	"strings"
	"time"

	sitesvc "github.com/0xJacky/Nginx-UI/internal/site"
	upstreamsvc "github.com/0xJacky/Nginx-UI/internal/upstream"
)

type configuredBackend struct {
	Service string
	Backend string
	Meta    backendMeta
}

func configuredBackendsByService(aliases map[string]backendMeta) map[string][]configuredBackend {
	result := make(map[string][]configuredBackend)
	seen := make(map[string]struct{})

	upstreamService := upstreamsvc.GetUpstreamService()

	for _, indexedSite := range sitesvc.GetAllIndexedSites() {
		services := servicesFromURLs(indexedSite.Urls)

		// The site index stores proxy targets per file. When a file contains
		// multiple server blocks, associating every target with every server_name
		// would create false topology. Only infer zero-traffic peers when the
		// config file represents exactly one logical service.
		if len(services) != 1 {
			continue
		}

		serviceName := services[0]

		for _, target := range indexedSite.ProxyTargets {
			if definition, ok := upstreamService.GetUpstreamDefinition(target.Host); ok {
				for _, server := range definition.Servers {
					addConfiguredBackend(result, seen, serviceName, server.Host, server.Port, definition.Name, aliases)
				}
				continue
			}

			addConfiguredBackend(result, seen, serviceName, target.Host, target.Port, "", aliases)
		}
	}

	for service := range result {
		sort.Slice(result[service], func(i, j int) bool {
			return result[service][i].Backend < result[service][j].Backend
		})
	}

	return result
}

func servicesFromURLs(urls []string) []string {
	set := make(map[string]struct{})

	for _, raw := range urls {
		u, err := neturl.Parse(raw)
		if err != nil {
			continue
		}

		service := normalizeService(u.Hostname())
		if service == "" {
			continue
		}
		set[service] = struct{}{}
	}

	services := make([]string, 0, len(set))
	for service := range set {
		services = append(services, service)
	}
	sort.Strings(services)
	return services
}

func addConfiguredBackend(
	result map[string][]configuredBackend,
	seen map[string]struct{},
	serviceName, host, port, upstreamName string,
	aliases map[string]backendMeta,
) {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" || port == "" {
		return
	}

	healthKey := joinSocket(host, port)
	backend := resolveBackendSocket(host, port)
	meta := aliases[backend]
	if meta.Name == "" {
		meta = aliases[healthKey]
	}
	if meta.Name == "" {
		meta.Name = strings.ToUpper(shortHostname(host)) + ":" + port
	}
	if meta.HealthKey == "" {
		meta.HealthKey = healthKey
	}
	if upstreamName != "" {
		meta.Upstreams = appendUnique(meta.Upstreams, upstreamName)
		sort.Strings(meta.Upstreams)
	}

	key := serviceName + "\x00" + backend
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}

	result[serviceName] = append(result[serviceName], configuredBackend{
		Service: serviceName,
		Backend: backend,
		Meta:    meta,
	})
}

func resolveBackendSocket(host, port string) string {
	cleanHost := strings.Trim(host, "[]")
	if net.ParseIP(cleanHost) != nil {
		return joinSocket(cleanHost, port)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	addresses, err := net.DefaultResolver.LookupHost(ctx, cleanHost)
	if err != nil || len(addresses) == 0 {
		return joinSocket(cleanHost, port)
	}

	// Prefer IPv4 because Nginx's $upstream_addr in this environment is
	// currently logged as IPv4:port.
	for _, address := range addresses {
		if ip := net.ParseIP(address); ip != nil && ip.To4() != nil {
			return joinSocket(address, port)
		}
	}

	return joinSocket(addresses[0], port)
}
