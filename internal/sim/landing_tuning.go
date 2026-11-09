package sim

// Playtest-tuned constants for the landing autopilot (ADR 0067, "Playtest-tuned
// constants"). One file because the table is one table: a playtest changes a
// number here, never a rule elsewhere. Every value is provisional until flown.
// Slice S1 reads only BrakeReserveFrac; the rest are declared now so later
// slices read them from here instead of writing literals.
const (
	// CueQuantumS: the finest step (s) the burn-at cue may move in; sets how
	// many refine rounds burnAtCue runs. 5 ms is ~8 m of stop point at orbital
	// speed. Playtest-tuned; found by the `miss:` jump diagnosis (fix-F).
	CueQuantumS = 0.005

	// BrakeReserveFrac: the throttle the cue, stop:, N and the autopilot plan
	// leave in reserve (1 - reserve = 0.9). ADR 0067 row 1; Q7.
	BrakeReserveFrac = 0.10

	// SiteShiftFarRangeM / SiteShiftMidRangeM: where the aim rung changes
	// (ground range to the computer's site). ADR 0067 row 2; Q6.
	SiteShiftFarRangeM = 50000.0
	SiteShiftMidRangeM = 5000.0
	// SiteShiftRungFarM / MidM / NearM: one arrow press at each rung. ADR 0067
	// row 3; Q6.
	SiteShiftRungFarM  = 1000.0
	SiteShiftRungMidM  = 250.0
	SiteShiftRungNearM = 25.0
	// SiteShiftFineM: one alt+arrow press, or a tenth of the rung when that is
	// smaller (2.5 m inside 5 km). Trial P42 (was a tenth everywhere).
	SiteShiftFineM = 10.0

	// NavSigma*M: 1-sigma (along, across) navigation error by era. ADR 0067
	// rows 4-6; Apollo 11 PDI, SLIM. A link at the draw or the ground update
	// makes an Apollo-era vessel modern-grade (P21), so Apollo 12's linked pair
	// (300, 500) is gone. The Apollo no-link pair mirrors the catalog's
	// nav_sigma_m on the command parts (a test pins them equal).
	NavSigmaApolloNoLinkAlongM  = 2000.0
	NavSigmaApolloNoLinkAcrossM = 1000.0
	NavSigmaModernAlongM        = 3.0
	NavSigmaModernAcrossM       = 3.0

	// SiteRevealRangeM: ground range inside which the view draws the real
	// flag. ADR 0067 row 7.
	SiteRevealRangeM = 10000.0

	// FinalPhaseGroundSpeedMps: ground speed under which the autopilot enters
	// final. ADR 0067 row 8; Q5.
	FinalPhaseGroundSpeedMps = 3.0
	// FinalRateAt100mMps / FinalRateUnder30mMps: the descent-rate schedule in
	// final. ADR 0067 row 9; Q5.
	FinalRateAt100mMps   = 3.0
	FinalRateUnder30mMps = 1.0

	// RODStepMps / RODFineStepMps: e/q and alt+e/alt+q under ROD. ADR 0067
	// row 10; Apollo ROD click 0.3 m/s.
	RODStepMps     = 1.0
	RODFineStepMps = 0.3

	// OutOfReachAuthorityFrac: pre-engage reach test, a fraction of the
	// stopping distance. ADR 0067 row 11; to be measured in S2.
	OutOfReachAuthorityFrac = 0.10

	// LMRCSThrustN / LMRCSMonopropKg: LM-derived landers' RCS figures. ADR
	// 0067 row 12; 16 x 445 N, two jets per axis (unverified).
	LMRCSThrustN    = 890.0
	LMRCSMonopropKg = 287.0

	// AutopilotWarpCap: warp while the autopilot burns. ADR 0067 row 13.
	AutopilotWarpCap = 10.0

	// LandingViewFitFrac / LandingViewFitFloorM: the landing view's entry fit.
	// ADR 0067 row 14; PROXIMITY's 2.25 / 25 m.
	LandingViewFitFrac   = 2.25
	LandingViewFitFloorM = 50.0
)
