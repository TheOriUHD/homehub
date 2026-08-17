package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/homehub/internal/integration"
	"github.com/TheOriUHD/homehub/internal/metrics"
)

// ---- upstream wire shapes --------------------------------------------------

type haConfig struct {
	LocationName string `json:"location_name"`
	Version      string `json:"version"`
	TimeZone     string `json:"time_zone"`
	Country      string `json:"country"`
}

type rawState struct {
	EntityID    string         `json:"entity_id"`
	State       string         `json:"state"`
	Attributes  map[string]any `json:"attributes"`
	LastChanged time.Time      `json:"last_changed"`
	LastUpdated time.Time      `json:"last_updated"`
}

type areaRow struct {
	AreaID string `json:"area_id"`
	Name   string `json:"name"`
}

type deviceRow struct {
	ID           string  `json:"id"`
	AreaID       *string `json:"area_id"`
	Name         *string `json:"name"`
	NameByUser   *string `json:"name_by_user"`
	Manufacturer *string `json:"manufacturer"`
	Model        *string `json:"model"`
}

type entityRow struct {
	EntityID       string  `json:"entity_id"`
	AreaID         *string `json:"area_id"`
	DeviceID       *string `json:"device_id"`
	Platform       string  `json:"platform"`
	Name           *string `json:"name"`
	OriginalName   *string `json:"original_name"`
	DisabledBy     *string `json:"disabled_by"`
	HiddenBy       *string `json:"hidden_by"`
	EntityCategory *string `json:"entity_category"`
}

// ---- normalized model ------------------------------------------------------

// Entity is the joined view of an entity's registry row and live state.
type Entity struct {
	EntityID   string         `json:"entityId"`
	Domain     string         `json:"domain"`
	ObjectID   string         `json:"objectId"`
	Name       string         `json:"name"`
	AreaID     string         `json:"areaId,omitempty"`
	Category   string         `json:"category,omitempty"` // "config" | "diagnostic"
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes"`
	LastChanged time.Time     `json:"lastChanged"`
	LastUpdated time.Time     `json:"lastUpdated"`
}

// Area is a Home Assistant area with the devices resolved out.
type Area struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Info is the instance overview.
type Info struct {
	Version      string `json:"version"`
	LocationName string `json:"locationName"`
	TimeZone     string `json:"timeZone,omitempty"`
	Country      string `json:"country,omitempty"`
}

// ---- connection ------------------------------------------------------------

type Conn struct {
	inst integration.Instance
	deps integration.Deps
	sock *socket

	mu         sync.RWMutex
	info       Info
	states     map[string]rawState
	areas      []areaRow
	entityMeta map[string]entityRow
	deviceArea map[string]*string
	refreshing bool
}

// bootstrap loads config, registries, and states, then subscribes to pushes.
func (c *Conn) bootstrap(ctx context.Context) error {
	var cfg haConfig
	if err := c.sock.callInto(ctx, map[string]any{"type": "get_config"}, &cfg); err != nil {
		return err
	}
	c.info = Info{Version: cfg.Version, LocationName: cfg.LocationName, TimeZone: cfg.TimeZone, Country: cfg.Country}

	if err := c.loadRegistries(ctx); err != nil {
		return err
	}
	var states []rawState
	if err := c.sock.callInto(ctx, map[string]any{"type": "get_states"}, &states); err != nil {
		return err
	}
	c.mu.Lock()
	c.states = make(map[string]rawState, len(states))
	for _, s := range states {
		c.states[s.EntityID] = s
	}
	c.mu.Unlock()

	if err := c.sock.subscribe(ctx, "state_changed", c.onStateChanged); err != nil {
		return err
	}
	// Registry edits are rare; a coalesced refetch keeps the model current.
	for _, ev := range []string{"area_registry_updated", "device_registry_updated", "entity_registry_updated"} {
		if err := c.sock.subscribe(ctx, ev, func(json.RawMessage) { c.scheduleRegistryRefresh() }); err != nil {
			return err
		}
	}
	return nil
}

func (c *Conn) loadRegistries(ctx context.Context) error {
	var areas []areaRow
	if err := c.sock.callInto(ctx, map[string]any{"type": "config/area_registry/list"}, &areas); err != nil {
		return err
	}
	var devices []deviceRow
	if err := c.sock.callInto(ctx, map[string]any{"type": "config/device_registry/list"}, &devices); err != nil {
		return err
	}
	var entities []entityRow
	if err := c.sock.callInto(ctx, map[string]any{"type": "config/entity_registry/list"}, &entities); err != nil {
		return err
	}
	sort.Slice(areas, func(i, j int) bool { return areas[i].Name < areas[j].Name })

	c.mu.Lock()
	c.areas = areas
	c.deviceArea = make(map[string]*string, len(devices))
	for _, d := range devices {
		c.deviceArea[d.ID] = d.AreaID
	}
	c.entityMeta = make(map[string]entityRow, len(entities))
	for _, e := range entities {
		c.entityMeta[e.EntityID] = e
	}
	c.mu.Unlock()
	return nil
}

// scheduleRegistryRefresh coalesces bursts of registry events into one
// refetch off the pump goroutine.
func (c *Conn) scheduleRegistryRefresh() {
	c.mu.Lock()
	if c.refreshing {
		c.mu.Unlock()
		return
	}
	c.refreshing = true
	c.mu.Unlock()

	go func() {
		time.Sleep(time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := c.loadRegistries(ctx)
		c.mu.Lock()
		c.refreshing = false
		c.mu.Unlock()
		if err == nil {
			c.deps.Publish("entities-changed", nil)
		}
	}()
}

// onStateChanged runs on the read pump: keep it allocation-light and fast.
func (c *Conn) onStateChanged(event json.RawMessage) {
	var ev struct {
		Data struct {
			EntityID string    `json:"entity_id"`
			NewState *rawState `json:"new_state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(event, &ev); err != nil || ev.Data.EntityID == "" {
		return
	}
	c.mu.Lock()
	if ev.Data.NewState == nil {
		delete(c.states, ev.Data.EntityID)
	} else {
		c.states[ev.Data.EntityID] = *ev.Data.NewState
	}
	c.mu.Unlock()

	if ev.Data.NewState == nil {
		c.deps.Publish("entities-changed", nil)
		return
	}
	c.deps.Publish("state", c.entityFor(*ev.Data.NewState))
}

// entityFor joins one raw state with its registry metadata.
func (c *Conn) entityFor(s rawState) Entity {
	domain, objectID, _ := strings.Cut(s.EntityID, ".")
	e := Entity{
		EntityID:    s.EntityID,
		Domain:      domain,
		ObjectID:    objectID,
		State:       s.State,
		Attributes:  s.Attributes,
		LastChanged: s.LastChanged,
		LastUpdated: s.LastUpdated,
	}
	if fn, ok := s.Attributes["friendly_name"].(string); ok && fn != "" {
		e.Name = fn
	} else {
		e.Name = objectID
	}
	c.mu.RLock()
	if meta, ok := c.entityMeta[s.EntityID]; ok {
		if meta.Name != nil && *meta.Name != "" {
			e.Name = *meta.Name
		}
		if meta.EntityCategory != nil {
			e.Category = *meta.EntityCategory
		}
		if meta.AreaID != nil && *meta.AreaID != "" {
			e.AreaID = *meta.AreaID
		} else if meta.DeviceID != nil {
			if areaID := c.deviceArea[*meta.DeviceID]; areaID != nil {
				e.AreaID = *areaID
			}
		}
	}
	c.mu.RUnlock()
	return e
}

// ---- API surface for handlers ----------------------------------------------

func (c *Conn) Info() Info {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.info
}

func (c *Conn) Areas() []Area {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Area, 0, len(c.areas))
	for _, a := range c.areas {
		out = append(out, Area{ID: a.AreaID, Name: a.Name})
	}
	return out
}

// Entities returns the joined entity list, hidden/disabled registry entries
// excluded, sorted by name for stable rendering.
func (c *Conn) Entities() []Entity {
	c.mu.RLock()
	raws := make([]rawState, 0, len(c.states))
	for _, s := range c.states {
		if meta, ok := c.entityMeta[s.EntityID]; ok {
			if meta.DisabledBy != nil || meta.HiddenBy != nil {
				continue
			}
		}
		raws = append(raws, s)
	}
	c.mu.RUnlock()

	out := make([]Entity, 0, len(raws))
	for _, s := range raws {
		out = append(out, c.entityFor(s))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].EntityID < out[j].EntityID
	})
	return out
}

// Entity returns one entity by id.
func (c *Conn) Entity(entityID string) (Entity, bool) {
	c.mu.RLock()
	s, ok := c.states[entityID]
	c.mu.RUnlock()
	if !ok {
		return Entity{}, false
	}
	return c.entityFor(s), true
}

// CallService invokes a Home Assistant service targeted at one entity.
func (c *Conn) CallService(ctx context.Context, domain, service, entityID string, data map[string]any) error {
	msg := map[string]any{
		"type":    "call_service",
		"domain":  domain,
		"service": service,
	}
	if len(data) > 0 {
		msg["service_data"] = data
	}
	if entityID != "" {
		msg["target"] = map[string]any{"entity_id": entityID}
	}
	_, err := c.sock.call(ctx, msg)
	if err != nil {
		return fmt.Errorf("%s.%s failed: %s", domain, service, friendlyError(err))
	}
	return nil
}

// ---- integration.Connection ------------------------------------------------

func (c *Conn) Health(ctx context.Context) integration.Health {
	if err := c.sock.dead(); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.sock.ping(pctx); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	return integration.Health{Status: integration.StatusConnected}
}

// Resources publishes one RBAC node per entity: <instance>/<domain>/<object>,
// so grants scope naturally to a whole domain or a single entity.
func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	root := c.inst.ResourcePath()
	entities := c.Entities()
	out := make([]integration.Resource, 0, len(entities))
	for _, e := range entities {
		out = append(out, integration.Resource{
			Path: root + "/" + e.Domain + "/" + e.ObjectID,
			Kind: e.Domain,
			Name: e.Name,
		})
	}
	return out, nil
}

// Collect samples numeric sensors (unit from the entity), binary sensors as
// 0/1, and climate temperatures — the series alerting and history run on.
func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	if err := c.sock.dead(); err != nil {
		return nil, err
	}
	now := time.Now()
	root := c.inst.ResourcePath()

	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []metrics.Sample
	for _, s := range c.states {
		if s.State == "unavailable" || s.State == "unknown" {
			continue
		}
		domain, objectID, _ := strings.Cut(s.EntityID, ".")
		resource := root + "/" + domain + "/" + objectID
		switch domain {
		case "sensor":
			v, err := strconv.ParseFloat(s.State, 64)
			if err != nil {
				continue
			}
			unit, _ := s.Attributes["unit_of_measurement"].(string)
			metric := "value"
			if dc, ok := s.Attributes["device_class"].(string); ok && dc != "" {
				metric = dc
			}
			out = append(out, metrics.Sample{Resource: resource, Metric: metric, Unit: unit, TS: now, Value: v})
		case "binary_sensor":
			v := 0.0
			if s.State == "on" {
				v = 1
			}
			out = append(out, metrics.Sample{Resource: resource, Metric: "state", Unit: "", TS: now, Value: v})
		case "climate":
			if cur, ok := numericAttr(s.Attributes, "current_temperature"); ok {
				out = append(out, metrics.Sample{Resource: resource, Metric: "current_temperature", Unit: "°C", TS: now, Value: cur})
			}
			if target, ok := numericAttr(s.Attributes, "temperature"); ok {
				out = append(out, metrics.Sample{Resource: resource, Metric: "target_temperature", Unit: "°C", TS: now, Value: target})
			}
		}
	}
	return out, nil
}

func numericAttr(attrs map[string]any, key string) (float64, bool) {
	switch v := attrs[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

func (c *Conn) Close() {
	c.sock.close()
}
