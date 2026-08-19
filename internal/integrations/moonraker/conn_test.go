package moonraker

import (
	"encoding/json"
	"testing"
)

// fields builds an objFields from a JSON object literal.
func fields(t *testing.T, js string) objFields {
	t.Helper()
	var f objFields
	if err := json.Unmarshal([]byte(js), &f); err != nil {
		t.Fatalf("bad fixture %q: %v", js, err)
	}
	return f
}

// TestBuildSnapshotDynamic feeds a deliberately varied printer — two extruders,
// a bed, a chamber heater, a couple of sensors, several fan kinds, a Neopixel,
// an output pin, and macros — and checks every object lands in the right
// bucket. This is the guarantee behind "works with any Klipper setup".
func TestBuildSnapshotDynamic(t *testing.T) {
	objects := []string{
		"extruder", "extruder1", "heater_bed", "heater_generic chamber_heater",
		"temperature_sensor raspberry_pi", "temperature_sensor mcu_temp",
		"temperature_fan exhaust",
		"fan", "fan_generic filter", "heater_fan hotend_fan", "controller_fan electronics",
		"neopixel status_led", "output_pin caselight",
		"gcode_macro START_PRINT", "gcode_macro LOAD_FILAMENT",
		"print_stats", "display_status", "virtual_sdcard", "toolhead", "gcode_move", "webhooks",
		"bed_mesh", "quad_gantry_level", // unclassified → extra
	}
	state := map[string]objFields{
		"extruder":                        fields(t, `{"temperature":210.4,"target":215,"power":0.62}`),
		"extruder1":                       fields(t, `{"temperature":25.0,"target":0,"power":0}`),
		"heater_bed":                      fields(t, `{"temperature":60.1,"target":60,"power":0.35}`),
		"heater_generic chamber_heater":   fields(t, `{"temperature":40.2,"target":45,"power":0.5}`),
		"temperature_sensor raspberry_pi": fields(t, `{"temperature":48.3,"measured_min_temp":40.0,"measured_max_temp":55.0}`),
		"temperature_sensor mcu_temp":     fields(t, `{"temperature":33.1}`),
		"temperature_fan exhaust":         fields(t, `{"temperature":41.0,"target":50,"speed":0.8}`),
		"fan":                             fields(t, `{"speed":0.5}`),
		"fan_generic filter":              fields(t, `{"speed":1.0,"rpm":4200}`),
		"heater_fan hotend_fan":           fields(t, `{"speed":1.0}`),
		"controller_fan electronics":      fields(t, `{"speed":0.3}`),
		"neopixel status_led":             fields(t, `{"color_data":[[1.0,0.5,0.0,0.0],[0,0,0,0]]}`),
		"output_pin caselight":            fields(t, `{"value":1.0}`),
		"print_stats":                     fields(t, `{"state":"printing","filename":"benchy.gcode","filament_used":1234.5,"print_duration":600,"total_duration":650,"info":{"current_layer":12,"total_layer":120}}`),
		"display_status":                  fields(t, `{"progress":0.42}`),
		"virtual_sdcard":                  fields(t, `{"progress":0.4321}`),
		"toolhead":                        fields(t, `{"position":[100,120,5,42],"homed_axes":"xyz","max_velocity":300}`),
		"gcode_move":                      fields(t, `{"speed_factor":1.0,"extrude_factor":0.98,"speed":1800}`),
		"webhooks":                        fields(t, `{"state":"ready","state_message":"Printer is ready"}`),
	}
	macros := macrosFrom(objects)

	snap := buildSnapshot(objects, state, macros)

	if len(snap.Heaters) != 4 {
		t.Fatalf("heaters: want 4 (2 extruders, bed, chamber), got %d: %+v", len(snap.Heaters), snap.Heaters)
	}
	// The bare extruder should be labelled "Extruder", extruder1 "Extruder 1".
	byObj := map[string]Heater{}
	for _, h := range snap.Heaters {
		byObj[h.Object] = h
	}
	if byObj["extruder"].Name != "Extruder" || byObj["extruder"].Kind != "extruder" {
		t.Errorf("extruder mislabelled: %+v", byObj["extruder"])
	}
	if byObj["extruder1"].Name != "Extruder 1" {
		t.Errorf("extruder1 label: got %q", byObj["extruder1"].Name)
	}
	if byObj["heater_bed"].Kind != "bed" {
		t.Errorf("bed kind: got %q", byObj["heater_bed"].Kind)
	}
	if byObj["heater_generic chamber_heater"].Name != "Chamber heater" {
		t.Errorf("chamber label: got %q", byObj["heater_generic chamber_heater"].Name)
	}
	if got := byObj["extruder"].Temp; got != 210.4 {
		t.Errorf("extruder temp: got %v", got)
	}

	if len(snap.Sensors) != 2 {
		t.Errorf("sensors: want 2, got %d", len(snap.Sensors))
	}
	if len(snap.TempFans) != 1 {
		t.Errorf("tempFans: want 1, got %d", len(snap.TempFans))
	}
	if len(snap.Fans) != 4 {
		t.Errorf("fans: want 4, got %d: %+v", len(snap.Fans), snap.Fans)
	}
	// Part & generic fans are controllable; heater/controller fans are not.
	fanByObj := map[string]Fan{}
	for _, f := range snap.Fans {
		fanByObj[f.Object] = f
	}
	if !fanByObj["fan"].Controllable || !fanByObj["fan_generic filter"].Controllable {
		t.Errorf("part/generic fans should be controllable")
	}
	if fanByObj["heater_fan hotend_fan"].Controllable || fanByObj["controller_fan electronics"].Controllable {
		t.Errorf("heater/controller fans should not be controllable")
	}
	if fanByObj["fan_generic filter"].RPM == nil || *fanByObj["fan_generic filter"].RPM != 4200 {
		t.Errorf("generic fan rpm not captured")
	}

	if len(snap.Leds) != 1 || snap.Leds[0].Pixels != 2 {
		t.Fatalf("leds: want 1 with 2 pixels, got %+v", snap.Leds)
	}
	if snap.Leds[0].Color[0] != 1.0 || snap.Leds[0].Color[1] != 0.5 {
		t.Errorf("led color: got %+v", snap.Leds[0].Color)
	}
	if len(snap.OutputPins) != 1 || snap.OutputPins[0].Value != 1.0 {
		t.Errorf("output pin: got %+v", snap.OutputPins)
	}
	if len(snap.Macros) != 2 {
		t.Errorf("macros: want 2, got %d", len(snap.Macros))
	}

	if snap.Print == nil || snap.Print.State != "printing" || snap.Print.Filename != "benchy.gcode" {
		t.Fatalf("print job not parsed: %+v", snap.Print)
	}
	if snap.Print.CurrentLayer != 12 || snap.Print.TotalLayer != 120 {
		t.Errorf("layer info: got %d/%d", snap.Print.CurrentLayer, snap.Print.TotalLayer)
	}
	// virtual_sdcard progress (0.4321) wins over display_status (0.42).
	if snap.Print.Progress != 0.4321 {
		t.Errorf("progress: want virtual_sdcard 0.4321, got %v", snap.Print.Progress)
	}

	if snap.Toolhead == nil || snap.Toolhead.HomedAxes != "xyz" {
		t.Fatalf("toolhead not parsed: %+v", snap.Toolhead)
	}
	if len(snap.Toolhead.Position) != 4 || snap.Toolhead.Position[0] != 100 {
		t.Errorf("toolhead position: got %+v", snap.Toolhead.Position)
	}
	if snap.Toolhead.ExtrudeFactor != 0.98 {
		t.Errorf("extrude factor from gcode_move: got %v", snap.Toolhead.ExtrudeFactor)
	}
	if snap.Toolhead.FanSpeed == nil || *snap.Toolhead.FanSpeed != 0.5 {
		t.Errorf("toolhead fan speed from `fan`: got %+v", snap.Toolhead.FanSpeed)
	}

	// bed_mesh & quad_gantry_level have no dedicated control → "extra".
	if len(snap.Extra) != 2 {
		t.Errorf("extra: want 2 (bed_mesh, quad_gantry_level), got %v", snap.Extra)
	}
}

func TestSubscribeSetSkipsMacros(t *testing.T) {
	objects := []string{"extruder", "gcode_macro FOO", "temperature_sensor x", "configfile", "mcu"}
	got := subscribeSet(objects)
	for _, o := range got {
		if o == "gcode_macro FOO" || o == "configfile" || o == "mcu" {
			t.Errorf("subscribeSet should skip %q", o)
		}
	}
	// extruder + sensor should be present.
	if len(got) != 2 {
		t.Errorf("want 2 subscribed objects, got %v", got)
	}
}
