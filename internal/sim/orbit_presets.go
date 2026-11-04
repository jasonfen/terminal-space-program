package sim

import (
	"embed"
	"encoding/json"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// Orbit presets (ADR 0065 decision 3): authored named ellipses the spawn
// form offers as a stop past the Orbit Ceiling. Sim data, not body data:
// sol.json feeds body_catalog_hash, so a field there would reject every save.
// Embedded only; shortcut: no user overlay, add one when a mod asks for it.

//go:embed data/orbit_presets.json
var orbitPresetsFS embed.FS

// OrbitPreset is one authored ellipse, elements in the body's equatorial
// frame with the ascending node on body-frame +X (RingState's node line).
type OrbitPreset struct {
	BodyID     string  `json:"body_id"`
	Key        string  `json:"key"`
	Label      string  `json:"label"`
	AM         float64 `json:"a_m"`
	E          float64 `json:"e"`
	InclDeg    float64 `json:"incl_deg"`
	ArgPeriDeg float64 `json:"arg_peri_deg"`
	Note       string  `json:"note"`
}

// ApoAltM and PeriAltM are altitudes above the body's mean radius.
func (p OrbitPreset) ApoAltM(b bodies.CelestialBody) float64 {
	return p.AM*(1+p.E) - b.RadiusMeters()
}
func (p OrbitPreset) PeriAltM(b bodies.CelestialBody) float64 {
	return p.AM*(1-p.E) - b.RadiusMeters()
}

// PeriodS is the Keplerian period about b.
func (p OrbitPreset) PeriodS(b bodies.CelestialBody) float64 {
	return orbital.PeriodFromA(p.AM, b.GravitationalParameter())
}

func (p OrbitPreset) elements() orbital.Elements {
	return orbital.Elements{A: p.AM, E: p.E, I: p.InclDeg * math.Pi / 180, Arg: p.ArgPeriDeg * math.Pi / 180}
}

// normalWorld is the preset's orbit-plane normal in world coordinates.
func (p OrbitPreset) normalWorld(frame orbital.BodyFrame) orbital.Vec3 {
	i := p.InclDeg * math.Pi / 180
	return frame.ToWorld(orbital.Vec3{Y: -math.Sin(i), Z: math.Cos(i)})
}

var orbitPresets = func() []OrbitPreset {
	data, err := orbitPresetsFS.ReadFile("data/orbit_presets.json")
	if err != nil {
		panic("sim: embedded orbit_presets.json missing: " + err.Error())
	}
	var cat struct {
		Presets []OrbitPreset `json:"presets"`
	}
	if err := json.Unmarshal(data, &cat); err != nil {
		panic("sim: embedded orbit_presets.json parse: " + err.Error())
	}
	return cat.Presets
}()

// OrbitPresetsFor returns b's presets (empty when b is not in sys).
func OrbitPresetsFor(sys bodies.System, b bodies.CelestialBody) []OrbitPreset {
	if sys.FindBody(b.ID) == nil {
		return nil
	}
	var out []OrbitPreset
	for _, p := range orbitPresets {
		if p.BodyID == b.ID {
			out = append(out, p)
		}
	}
	return out
}

// OrbitPresetByKey finds a body's preset by key ("frozen").
func OrbitPresetByKey(sys bodies.System, b bodies.CelestialBody, key string) (OrbitPreset, bool) {
	for _, p := range OrbitPresetsFor(sys, b) {
		if p.Key == key {
			return p, true
		}
	}
	return OrbitPreset{}, false
}

// presetSlot counts the vessels already on p's ellipse about primary: same
// body, a within 1 %, e within 0.02, plane normal within 2 degrees.
func (w *World) presetSlot(primary bodies.CelestialBody, p OrbitPreset) int {
	frame := orbital.ReferenceFrameForPrimary(primary)
	mu := primary.GravitationalParameter()
	want := p.normalWorld(frame)
	k := 0
	for _, c := range w.Crafts {
		if c.Primary.ID != primary.ID || c.SystemIdx != w.SystemIdx {
			continue
		}
		el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
		if el.E >= 1 || math.Abs(el.A-p.AM) > 0.01*p.AM || math.Abs(el.E-p.E) > 0.02 {
			continue
		}
		h := c.State.R.Cross(c.State.V).Unit()
		if math.Acos(math.Max(-1, math.Min(1, h.Dot(want)))) > 2*math.Pi/180 {
			continue
		}
		k++
	}
	return k
}

// presetState places a vessel on p at slot k: apolune for slot 0, then one
// fifth of a lap (72 degrees of mean anomaly) behind per slot, wrapping at 5.
func presetState(primary bodies.CelestialBody, p OrbitPreset, k int) (orbital.Vec3, orbital.Vec3) {
	frame := orbital.ReferenceFrameForPrimary(primary)
	m := math.Mod(180-72*float64(k%5)+360, 360) * math.Pi / 180
	nu := orbital.TrueAnomaly(orbital.SolveKepler(m, p.E), p.E)
	el := p.elements()
	r := orbital.PositionAtTrueAnomaly(el, nu)
	v := orbital.VelocityAtTrueAnomaly(el, nu, primary.GravitationalParameter())
	return frame.ToWorld(r), frame.ToWorld(v)
}
