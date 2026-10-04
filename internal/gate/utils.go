package gate

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"

	"github.com/mihavo/gct/internal/config"
)

func buildRoutingTable(configs []config.RoutesConfig) (RoutingTable, error) {
	table := RoutingTable{
		routes: make([]Route, 0, len(configs)),
	}
	seen := make(map[string]bool, len(configs))

	for _, rc := range configs {
		if seen[rc.Prefix] {
			return RoutingTable{}, fmt.Errorf("route %q: duplicate prefix %q", rc.Name, rc.Prefix)
		}
		seen[rc.Prefix] = true

		upstream, err := url.Parse(rc.Location)
		if err != nil {
			return RoutingTable{}, fmt.Errorf("route %q: parsing location: %w", rc.Name, err)
		}

		if upstream.Scheme == "" || upstream.Host == "" {
			return RoutingTable{}, fmt.Errorf("route %q: location %q must be an absolute URL such as http://host:port/path", rc.Name, rc.Location)
		}

		table.routes = append(table.routes, Route{
			Name:     rc.Name,
			Prefix:   rc.Prefix,
			Upstream: upstream,
		})
	}

	slices.SortFunc(table.routes, func(a, b Route) int {
		return cmp.Compare(len(b.Prefix), len(a.Prefix))
	})

	return table, nil
}
