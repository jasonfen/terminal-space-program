package spacecraft

// ADR 0070: an engine's Isp blends from its sea-level figure to its vacuum
// figure with the ambient pressure, and the mass flow stays fixed, so thrust
// follows Isp (F = mdot g0 Isp).

// SeaLevelPressurePa is Earth's sea-level pressure, the p_sl of the blend.
const SeaLevelPressurePa = 101325.0

// IspAt is Isp_vac - (Isp_vac - Isp_sl) * pr, where pr = p / p_sl, never below
// zero (Venus's surface is past any engine).
func IspAt(sl, vac, pr float64) float64 {
	return max(0, vac-(vac-sl)*pr)
}

// SetAmbient sets Thrust and Isp for the pressure ratio pr = p / p_sl and
// reports whether either moved. A flat engine (no figures, or sl == vac) is
// left exactly as it is.
func (st *Stage) SetAmbient(pr float64) bool {
	if st.IspVac <= 0 || st.ThrustVac <= 0 || st.IspSL == st.IspVac {
		return false
	}
	isp := IspAt(st.IspSL, st.IspVac, pr)
	thrust := st.ThrustVac * isp / st.IspVac
	if thrust == st.Thrust && isp == st.Isp {
		return false
	}
	st.Thrust, st.Isp = thrust, isp
	return true
}

// SetAmbient puts every stage's engine at pressure ratio pr and refreshes the
// vessel's flat Thrust and Isp (and only those) when anything moved. Call it where the vessel is, once
// per tick; the SuperDraco swap (onEngine) happens after it, never inside.
func (s *Spacecraft) SetAmbient(pr float64) {
	moved := false
	for i := range s.Stages {
		if s.Stages[i].SetAmbient(pr) {
			moved = true
		}
	}
	if moved {
		s.Thrust, s.Isp = LitEngine(s.Stages)
	}
}

// EngineAt is the lit set's thrust and Isp at pressure ratio pr, whatever air
// the vessel is in now: what a forecast reads for a burn that happens
// elsewhere. While the SuperDracos are swapped in they are flat, so it is
// the live pair.
func (s *Spacecraft) EngineAt(pr float64) (thrust, isp float64) {
	if len(s.Stages) == 0 {
		return s.Thrust, s.Isp
	}
	if lit := s.Stages[s.LitStage()]; lit.SuperThrust > 0 && lit.Thrust == lit.SuperThrust {
		return s.Thrust, s.Isp
	}
	cp := append([]Stage(nil), s.Stages...)
	for i := range cp {
		cp[i].SetAmbient(pr)
	}
	return LitEngine(cp)
}

// EngineCurve returns EngineAt as a cheap function of pr for a forecast's inner
// loop: both figures are linear in pr (fixed mass flow), so two evaluations
// fix the line.
func (s *Spacecraft) EngineCurve() func(pr float64) (thrust, isp float64) {
	t0, i0 := s.EngineAt(0)
	t1, i1 := s.EngineAt(1)
	return func(pr float64) (float64, float64) {
		return max(0, t0+(t1-t0)*pr), max(0, i0+(i1-i0)*pr)
	}
}

// RestoreEngine rebuilds the catalog figures after a load (the save keeps only
// the live Thrust and Isp). The Isp pair comes from the catalog stage the
// loadout and name identify, and only when the saved Isp lies inside it; the
// mass flow is what the saved pair says, so a halved booster stays halved.
// An unknown stage stays flat at its saved figures.
func (s *Stage) RestoreEngine() {
	if s.IspVac > 0 || s.Thrust <= 0 || s.Isp <= 0 {
		return
	}
	d, ok := spriteIdx.lookup(s.LoadoutID, s.Name)
	if !ok || d.ispVac <= 0 || d.ispSL == d.ispVac {
		return
	}
	lo, hi := min(d.ispSL, d.ispVac), max(d.ispSL, d.ispVac)
	if s.Isp < lo-0.5 || s.Isp > hi+0.5 {
		return
	}
	s.IspSL, s.IspVac = d.ispSL, d.ispVac
	s.ThrustVac = s.Thrust * d.ispVac / s.Isp
}
