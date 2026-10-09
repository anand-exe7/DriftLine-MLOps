package prediction

import (
	"context"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Role says why a route receives traffic.
type Role string

const (
	RolePrimary    Role = "primary"    // the main running deployment
	RoleCanary     Role = "canary"     // a new version getting a slice of traffic
	RoleProduction Role = "production" // no deployments: fall back to the registry's production version
)

// Route is one destination for a model's traffic.
type Route struct {
	DeploymentID *uuid.UUID `json:"deployment_id,omitempty"`
	VersionID    uuid.UUID  `json:"version_id"`
	ModelName    string     `json:"model_name"`
	Version      string     `json:"version"`
	Weight       int        `json:"weight"` // percent, routes of a model sum to 100
	Role         Role       `json:"role"`
}

// RouteSource supplies the current routing table for a model. The deployment
// manager implements it; the router only caches it.
type RouteSource interface {
	Routes(ctx context.Context, model string) ([]Route, error)
}

// Router picks a route per request. Routing tables are cached for `ttl` and
// invalidated immediately when a deployment changes.
type Router struct {
	src RouteSource
	ttl time.Duration

	mu    sync.RWMutex
	cache map[string]cachedRoutes

	// One counter per model, shared by every request goroutine. atomic.Uint64
	// lets thousands of concurrent requests bump it without a lock.
	counters sync.Map // model -> *atomic.Uint64
}

type cachedRoutes struct {
	routes  []Route
	fetched time.Time
}

func NewRouter(src RouteSource, ttl time.Duration) *Router {
	return &Router{src: src, ttl: ttl, cache: map[string]cachedRoutes{}}
}

// Invalidate drops a model's cached routes (called on deployment changes).
func (r *Router) Invalidate(model string) {
	r.mu.Lock()
	delete(r.cache, model)
	r.mu.Unlock()
}

// Routes returns the (possibly cached) routing table.
func (r *Router) Routes(ctx context.Context, model string) ([]Route, error) {
	r.mu.RLock()
	c, ok := r.cache[model]
	r.mu.RUnlock()
	if ok && time.Since(c.fetched) < r.ttl {
		return c.routes, nil
	}

	routes, err := r.src.Routes(ctx, model)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cache[model] = cachedRoutes{routes: routes, fetched: time.Now()}
	r.mu.Unlock()
	return routes, nil
}

// Pick chooses a route. With a routing key (e.g. a customer id) the choice is
// sticky: the same key always lands on the same version while weights are
// unchanged. Without one, a per-model atomic counter spreads requests so a
// 90/10 split is exact over every 100 requests rather than only on average.
func (r *Router) Pick(model, routingKey string, routes []Route) (Route, bool) {
	if len(routes) == 0 {
		return Route{}, false
	}
	if len(routes) == 1 {
		return routes[0], true
	}

	var slot uint64
	if routingKey != "" {
		h := fnv.New32a()
		h.Write([]byte(routingKey))
		slot = uint64(h.Sum32()) % 100
	} else {
		v, _ := r.counters.LoadOrStore(model, new(atomic.Uint64))
		// Multiply by a number coprime with 100 to interleave slots, so a
		// 10% canary gets every ~10th request instead of a burst of 10.
		slot = (v.(*atomic.Uint64).Add(1) * 37) % 100
	}

	var cum uint64
	for _, rt := range routes {
		cum += uint64(rt.Weight)
		if slot < cum {
			return rt, true
		}
	}
	return routes[len(routes)-1], true
}
