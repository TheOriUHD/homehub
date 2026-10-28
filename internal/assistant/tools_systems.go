package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Per-system depth and control. Every tool here rides Toolbox.Call — an
// in-process request to HomeHub's own API under the calling user's
// identity — so RBAC, auditing, and error messages are the handlers',
// not a re-implementation. A 403 comes back to the model as a readable
// refusal; every action lands in the audit log under the human's name.

// maxToolResultBytes caps what one tool feeds back into the model. Tool output
// is prompt tokens, and prompt tokens are money on a cloud model and a hard
// wall on a local one — see clampToolResult, which holds every tool to this,
// not only the ones that remember to call capResult.
const maxToolResultBytes = 7000

func (tb *Toolbox) systemTools() []tool {
	return []tool{
		{
			def: ToolDef{
				Name:        "system_details",
				Description: "Full live detail for one system by id (from list_systems): containers for Docker, pools/disks for TrueNAS, players for Minecraft, guests for Proxmox, cameras for the NVR, devices for Tailscale, sites for Caddy, stats for DNS, instances for AMP, databases for DB servers. For Home Assistant use ha_entities instead.",
				InputSchema: obj(`"systemId":{"type":"string"}`, "systemId"),
			},
			run: tb.systemDetails,
		},
		{
			def: ToolDef{
				Name:        "ha_entities",
				Description: "Search Home Assistant entities: id, name, state, area. Filter by domain (light, switch, sensor, climate, …) and/or a search string. Returns at most `limit` (default 25).",
				InputSchema: obj(`"systemId":{"type":"string"},"domain":{"type":"string"},"search":{"type":"string"},"limit":{"type":"integer"}`, "systemId"),
			},
			run: tb.haEntities,
		},
		{
			def: ToolDef{
				Name:        "ha_call_service",
				Description: "Control a Home Assistant entity: service is the action in the entity's own domain (turn_on, turn_off, toggle, set_temperature, …), data carries service fields like brightness_pct or temperature. Unlocking a lock, disarming the alarm and opening a door, gate, garage or window are NOT executed — they are queued for the user to approve; tell them what you proposed.",
				InputSchema: obj(`"systemId":{"type":"string"},"entityId":{"type":"string"},"service":{"type":"string"},"data":{"type":"object","additionalProperties":true}`, "systemId", "entityId", "service"),
			},
			run: tb.haCallService,
		},
		{
			def: ToolDef{
				Name:        "docker_container_action",
				Description: "start, stop, restart, pause, or unpause one Docker container by name. start and unpause run at once; stop, restart and pause are queued for the user to approve.",
				InputSchema: obj(`"systemId":{"type":"string"},"name":{"type":"string"},"action":{"type":"string","enum":["start","stop","restart","pause","unpause"]}`, "systemId", "name", "action"),
			},
			run: tb.dockerAction,
		},
		{
			def: ToolDef{
				Name:        "mc_command",
				Description: "Run a console command on ONE server of a Minecraft network (serverId from system_details; no leading slash). The console has NO player context: @s/@p never work — use exact player names. Use modern item ids (enchanted_golden_apple, not NBT syntax). The result includes the console's reaction — READ it: an error there means the command failed even if dispatch was acknowledged. Commands that hand out power or take the server away (op, deop, ban, pardon, stop, reload, whitelist off, permission plugins, sudo) are queued for the user to approve." + mcEssentialsBriefing,
				InputSchema: obj(`"systemId":{"type":"string"},"serverId":{"type":"string","description":"The target server's uuid within the network"},"command":{"type":"string"}`, "systemId", "serverId", "command"),
			},
			run: tb.mcCommand,
		},
		{
			def: ToolDef{
				Name:        "mc_player_detail",
				Description: "Full live detail for one player on a Minecraft server: position, vitals, gamemode, active effects, complete inventory (items, armor, offhand, ender chest) and statistics (playtime, deaths, kills, distances). Needs the player uuid (from system_details / the server's player list) and the server to be Paper/Spigot.",
				InputSchema: obj(`"systemId":{"type":"string"},"serverId":{"type":"string"},"playerUuid":{"type":"string"}`, "systemId", "serverId", "playerUuid"),
			},
			run: tb.mcPlayerDetail,
		},
		{
			def: ToolDef{
				Name:        "mc_power",
				Description: "Start, stop, or restart ONE Minecraft server over the network's SSH access (start/relaunch from its start.sh; stop = graceful console stop). Needs SSH configured. Disruptive (stop/restart drop players) — only on the user's explicit request; stop and restart are queued for the user to approve.",
				InputSchema: obj(`"systemId":{"type":"string"},"serverId":{"type":"string"},"action":{"type":"string","enum":["start","stop","restart"]}`, "systemId", "serverId", "action"),
			},
			run: tb.mcPower,
		},
		{
			def: ToolDef{
				Name:        "amp_instance_action",
				Description: "start, stop, or restart one AMP game-server instance (instanceId from system_details). start runs at once; stop and restart are queued for the user to approve.",
				InputSchema: obj(`"systemId":{"type":"string"},"instanceId":{"type":"string"},"action":{"type":"string","enum":["start","stop","restart"]}`, "systemId", "instanceId", "action"),
			},
			run: tb.ampAction,
		},
		{
			def: ToolDef{
				Name:        "pve_guest_action",
				Description: "start, shutdown, stop, or reboot one Proxmox VM/container by vmid. shutdown is graceful; stop pulls the plug. start runs at once; shutdown, stop and reboot are queued for the user to approve.",
				InputSchema: obj(`"systemId":{"type":"string"},"vmid":{"type":"integer"},"action":{"type":"string","enum":["start","shutdown","stop","reboot"]}`, "systemId", "vmid", "action"),
			},
			run: tb.pveAction,
		},
		{
			def: ToolDef{
				Name:        "dns_blocking",
				Description: "Enable or disable DNS ad-blocking on a Pi-hole/AdGuard system, optionally only for `minutes`.",
				InputSchema: obj(`"systemId":{"type":"string"},"enabled":{"type":"boolean"},"minutes":{"type":"integer"}`, "systemId", "enabled"),
			},
			run: tb.dnsBlocking,
		},
	}
}

// call runs one in-process API request and normalizes the outcome: 2xx
// returns the body (or ok), anything else becomes an error carrying the
// handler's own message.
func (tb *Toolbox) call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	if tb.Call == nil {
		return nil, fmt.Errorf("system tools are unavailable in this context")
	}
	status, raw := tb.Call(ctx, method, path, body)
	if status >= 200 && status < 300 {
		if len(raw) == 0 {
			return json.RawMessage(`{"ok":true}`), nil
		}
		return raw, nil
	}
	var parsed struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if parsed.Message != "" {
		return nil, fmt.Errorf("%s", parsed.Message)
	}
	return nil, fmt.Errorf("the request failed with HTTP %d", status)
}

func parseSystemID(input json.RawMessage) (uuid.UUID, json.RawMessage, error) {
	var args struct {
		SystemID string `json:"systemId"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return uuid.Nil, input, fmt.Errorf("systemId is required")
	}
	id, err := uuid.Parse(strings.TrimSpace(args.SystemID))
	if err != nil {
		return uuid.Nil, input, fmt.Errorf("systemId must be the uuid from list_systems")
	}
	return id, input, nil
}

// capResult keeps tool output inside the token budget; the model is told
// when it saw a truncated view.
func capResult(raw json.RawMessage) any {
	if len(raw) <= maxToolResultBytes {
		return raw
	}
	return map[string]any{
		"truncated": true,
		"note":      fmt.Sprintf("response was %d bytes; showing the first part — ask more specifically", len(raw)),
		"partial":   string(raw[:maxToolResultBytes]),
	}
}

func (tb *Toolbox) systemDetails(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	id, _, err := parseSystemID(input)
	if err != nil {
		return nil, err
	}
	row, err := tb.Store.GetIntegration(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("no such system")
	}
	if !access.VisibleUnder(row.Type + "/" + row.ID.String()) {
		return nil, fmt.Errorf("no such system")
	}

	var path string
	switch row.Type {
	case "docker":
		path = "/docker/" + id.String() + "/containers"
	case "truenas":
		path = "/truenas/" + id.String() + "/overview"
	case "minecraft":
		path = "/mc/" + id.String() + "/overview"
	case "proxmox":
		path = "/proxmox/" + id.String() + "/guests"
	case "amp":
		path = "/amp/" + id.String() + "/overview"
	case "aperture":
		path = "/nvr/" + id.String() + "/overview"
	case "tailscale":
		path = "/ts/" + id.String() + "/devices"
	case "caddy":
		path = "/caddy/" + id.String() + "/sites"
	case "pihole", "adguard":
		raw, err := tb.call(ctx, http.MethodGet, "/dns/instances", nil)
		if err != nil {
			return nil, err
		}
		var all []map[string]any
		if json.Unmarshal(raw, &all) == nil {
			for _, inst := range all {
				if inst["id"] == id.String() {
					return inst, nil
				}
			}
		}
		return nil, fmt.Errorf("no such DNS system")
	case "mysql", "mongodb":
		path = "/db/" + id.String() + "/databases"
	case "homeassistant":
		return map[string]any{"note": "use ha_entities to search Home Assistant entities"}, nil
	default:
		return nil, fmt.Errorf("no detail view for type %s", row.Type)
	}

	raw, err := tb.call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return capResult(raw), nil
}

func (tb *Toolbox) haEntities(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	id, _, err := parseSystemID(input)
	if err != nil {
		return nil, err
	}
	var args struct {
		Domain string `json:"domain"`
		Search string `json:"search"`
		Limit  int    `json:"limit"`
	}
	_ = json.Unmarshal(input, &args)
	if args.Limit <= 0 {
		args.Limit = 25
	}
	if args.Limit > 50 {
		args.Limit = 50
	}

	path := "/ha/" + id.String() + "/entities"
	if args.Domain != "" {
		path += "?domain=" + url.QueryEscape(args.Domain)
	}
	raw, err := tb.call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	// The endpoint returns {areas, entities}, not a bare list.
	var payload struct {
		Entities []struct {
			EntityID string         `json:"entityId"`
			Name     string         `json:"name"`
			State    string         `json:"state"`
			Area     *string        `json:"area"`
			Attrs    map[string]any `json:"attributes"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("unexpected entities response")
	}
	entities := payload.Entities

	needle := strings.ToLower(strings.TrimSpace(args.Search))
	type slim struct {
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		State string         `json:"state"`
		Area  string         `json:"area,omitempty"`
		Attrs map[string]any `json:"attrs,omitempty"`
	}
	// The attributes worth the tokens; the rest stays home.
	keep := []string{"brightness", "color_temp_kelvin", "rgb_color", "temperature", "current_temperature", "unit_of_measurement", "device_class", "hvac_action", "percentage", "media_title"}
	out := []slim{}
	total := 0
	for _, e := range entities {
		if needle != "" &&
			!strings.Contains(strings.ToLower(e.EntityID), needle) &&
			!strings.Contains(strings.ToLower(e.Name), needle) {
			continue
		}
		total++
		if len(out) >= args.Limit {
			continue
		}
		s := slim{ID: e.EntityID, Name: e.Name, State: e.State}
		if e.Area != nil {
			s.Area = *e.Area
		}
		for _, k := range keep {
			if v, ok := e.Attrs[k]; ok && v != nil {
				if s.Attrs == nil {
					s.Attrs = map[string]any{}
				}
				s.Attrs[k] = v
			}
		}
		out = append(out, s)
	}
	return map[string]any{"matched": total, "shown": len(out), "entities": out}, nil
}

// The lifecycle and Home Assistant tools are plain controls: tools_control.go
// decides whether each call runs now or waits for the person.

func (tb *Toolbox) haCallService(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	return tb.control(ctx, KindHAService, input)
}

func (tb *Toolbox) dockerAction(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	return tb.control(ctx, KindDockerAction, input)
}

func (tb *Toolbox) mcPower(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	return tb.control(ctx, KindMCPower, input)
}

func (tb *Toolbox) ampAction(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	return tb.control(ctx, KindAMPAction, input)
}

func (tb *Toolbox) pveAction(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	return tb.control(ctx, KindPVEAction, input)
}

func (tb *Toolbox) mcCommand(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	held, req, err := tb.prepareControl(ctx, KindMCCommand, input)
	if err != nil {
		return nil, err
	}
	if held != nil {
		return held, nil
	}
	base := strings.TrimSuffix(req.Path, "/command")

	// Snapshot the console position, run the command, then read what the
	// server actually said about it — the ack alone reports dispatch, not
	// success, and "Done!" on a failed /give helps nobody.
	lastSeq := int64(-1)
	// nextSeq is the buffer head regardless of limit, so an over-the-head
	// read returns it with zero lines.
	if raw, err := tb.call(ctx, http.MethodGet, base+"/console?after=9000000000000000&limit=1", nil); err == nil {
		var console struct {
			NextSeq int64 `json:"nextSeq"`
		}
		if json.Unmarshal(raw, &console) == nil && console.NextSeq > 0 {
			lastSeq = console.NextSeq - 1
		}
	}

	ack, err := tb.call(ctx, req.Method, req.Path, req.Body)
	if err != nil {
		return nil, err
	}

	// Give the server a beat to print its reaction, then collect it.
	reaction := []string{}
	select {
	case <-ctx.Done():
	case <-time.After(700 * time.Millisecond):
		if raw, err := tb.call(ctx, http.MethodGet,
			fmt.Sprintf("%s/console?after=%d&limit=12", base, lastSeq), nil); err == nil {
			var console struct {
				Lines []struct {
					Text string `json:"text"`
				} `json:"lines"`
			}
			if json.Unmarshal(raw, &console) == nil {
				for _, line := range console.Lines {
					reaction = append(reaction, line.Text)
				}
			}
		}
	}
	return map[string]any{"ack": ack, "console": reaction}, nil
}

func (tb *Toolbox) mcPlayerDetail(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	id, _, err := parseSystemID(input)
	if err != nil {
		return nil, err
	}
	var args struct {
		ServerID   string `json:"serverId"`
		PlayerUUID string `json:"playerUuid"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("serverId and playerUuid are required")
	}
	serverID, err := uuid.Parse(strings.TrimSpace(args.ServerID))
	if err != nil {
		return nil, fmt.Errorf("serverId must be a server uuid from system_details")
	}
	if strings.TrimSpace(args.PlayerUUID) == "" {
		return nil, fmt.Errorf("playerUuid is required")
	}
	raw, err := tb.call(ctx, http.MethodGet,
		fmt.Sprintf("/mc/%s/servers/%s/players/%s", id.String(), serverID.String(), url.PathEscape(strings.TrimSpace(args.PlayerUUID))), nil)
	if err != nil {
		return nil, err
	}
	return capResult(raw), nil
}

func (tb *Toolbox) dnsBlocking(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	id, _, err := parseSystemID(input)
	if err != nil {
		return nil, err
	}
	var args struct {
		Enabled *bool `json:"enabled"`
		Minutes int   `json:"minutes"`
	}
	if err := json.Unmarshal(input, &args); err != nil || args.Enabled == nil {
		return nil, fmt.Errorf("enabled is required")
	}
	body := map[string]any{"enabled": *args.Enabled}
	if args.Minutes > 0 {
		body["minutes"] = args.Minutes
	}
	return tb.call(ctx, http.MethodPost, "/dns/"+id.String()+"/blocking", body)
}

// mcEssentialsBriefing teaches the model the EssentialsX admin vocabulary.
// Curated from EssentialsX 2.21 plugin.yml (+ EssentialsSpawn); usage lines
// are the plugin's own. Appended to the mc_command tool description.
const mcEssentialsBriefing = "\n\nPaper servers here typically run EssentialsX — PREFER its commands for player management. The console has no player context, so always pass the exact player name even where [player] looks optional. If the console reacts with 'Unknown command', EssentialsX is not installed on that server — improvise with vanilla equivalents (/effect give <p> …, /gamemode, /tp, /give, /xp add, /clear, /kill, /time set, /weather, /summon). EssentialsX reference (usage - effect):\n/god [player] [on|off] - Enables your godly powers.\n/heal [player] - Heals you or the given player.\n/feed [player] - Satisfy the hunger.\n/fly [player] [on|off] - Take off, and soar!\n/speed [type] <speed> [player] - Change your speed limits.\n/vanish [player] [on|off] - Hide yourself from other players.\n/gamemode <survival|creative|adventure|spectator> [player] - Change player gamemode.\n/invsee <player> - See the inventory of other players.\n/enderchest [player] - Lets you see inside an enderchest.\n/tp <player> [otherplayer] - Teleport to a player.\n/tppos <x> <y> <z> [yaw] [pitch] [world] - Teleport to coordinates.\n/tphere <player> - Teleport a player to you.\n/tpall [player] - Teleport all online players to another player.\n/back [player] - Teleports you to your location prior to tp/spawn/warp.\n/top - Teleport to the highest block at your current position.\n/spawn [player] - Bypass spawn teleportation on join when spawn-on-join is true\n/setspawn <group> - Sets the spawn point to your current position.\n/home [player:][name] - Teleport to your home.\n/sethome [[player:]name] - Set your home to your current location.\n/delhome [player:]<name> - Removes a home.\n/warp <pagenumber|warp> [player] - List all warps or warp to the specified location.\n/setwarp <warp> - Creates a new warp.\n/delwarp <warp> - Deletes the specified warp.\n/kit [kit] [player] - Obtains the specified kit or views all available kits.\n/give <player> <item|numeric> [amount [itemmeta...]] - Give a player an item.\n/item <item|numeric> [amount [itemmeta...]] - Spawn an item.\n/more [amount] - Fills the item stack in hand to specified amount, or to maximum size if none is specified.\n/repair [hand|all] - Repairs the durability of one or all items.\n/clearinventory [player|*] [item[:<data>]|*|**] [amount] - Clear all items in your inventory.\n/exp [reset|show|set|give] [playername [amount]] - Give, set, reset, or look at a players experience.\n/hat [remove] - Get some cool new headgear.\n/nick [player] <nickname|off> - Change your nickname or that of another player.\n/realname <nickname> - Displays the username of a user based on nick.\n/seen <playername> - Shows the last logout time of a player.\n/whois <nickname> - Determine basic information about the specified player.\n/near [playername] [radius] - Lists the players near by or around a player.\n/getpos [player] - Get your current coordinates or those of a player.\n/mute <player> [datediff] [reason] - Mutes or unmutes a player.\n/kick <player> [reason] - Kicks a specified player with a reason.\n/broadcast <msg> - Broadcasts a message to the entire server.\n/msg <to> <message> - Sends a private message to the specified player.\n/socialspy [player] [on|off] - Toggles if you can see msg/mail commands in chat.\n/sudo <player> <command [args]> - Make another user perform a command.\n/remove <all|tamed|named|drops|arrows|boats|minecarts|xp|paintings|itemframes|endercrystals|monsters|animals|ambient|mobs|[mobType]> [radius|world] - Removes entities in your world. (aliases: killall, butcher)\n/spawnmob <mob>[:data][,<mount>[:data]] [amount] [player] - Spawns a mob.\n/lightning [player] [power] - The power of Thor. Strike at cursor or player.\n/weather <storm/sun> [duration] - Sets the weather.\n/time [set|add] [day|night|dawn|17:30|4pm|4000ticks] [worldname|all] - Display/Change the world time. Defaults to current world.\n/ptime [list|reset|day|night|dawn|17:30|4pm|4000ticks] [player|*] - Adjust player's client time. Add @ prefix to fix.\n/pweather [list|reset|storm|sun|clear] [player|*] - Adjust a player's weather\n/afk [player/message...] - Marks you as away-from-keyboard.\n/suicide - Causes you to perish.\n/workbench - Opens up a workbench.\n/balance [player] - States the current balance of a player.\n/pay <player> <amount> - Pays another player from your balance.\n/balancetop [page] - Gets the top balance values.\n/eco <give|take|set|reset> <player> <amount> - Manages the server economy."
