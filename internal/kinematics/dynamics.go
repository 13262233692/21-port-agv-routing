package kinematics

import (
	"math"
)

const (
	DefaultTrackWidth      = 2.0
	DefaultWheelbase       = 4.0
	DefaultAGVTareMass     = 18000.0
	DefaultMaxSpeed        = 6.0
	DefaultTurnSpeed       = 1.5
	DefaultGravity         = 9.81

	MaxCentripetalAccelNormal    = 2.0
	MaxCentripetalAccelModerate  = 1.5
	MaxCentripetalAccelSevere    = 1.0
	MaxCentripetalAccelCritical  = 0.5

	MinTurnRadius = 2.0
)

type RolloverRisk int

const (
	RiskNone RolloverRisk = iota
	RiskLow
	RiskModerate
	RiskHigh
	RiskCritical
)

type DynamicsParams struct {
	TrackWidth          float64
	Wheelbase           float64
	AGVTareMass         float64
	Gravity             float64
	MaxCentripetalAccel float64
}

func DefaultDynamicsParams() *DynamicsParams {
	return &DynamicsParams{
		TrackWidth:          DefaultTrackWidth,
		Wheelbase:           DefaultWheelbase,
		AGVTareMass:         DefaultAGVTareMass,
		Gravity:             DefaultGravity,
		MaxCentripetalAccel: MaxCentripetalAccelNormal,
	}
}

func RolloverThresholdSpeed(trackWidth, cogHeight, gravity, turnRadius float64) float64 {
	if turnRadius <= 0 || cogHeight <= 0 || gravity <= 0 {
		return 0
	}
	return math.Sqrt(gravity * turnRadius * trackWidth / (2.0 * cogHeight))
}

func MaxSafeSpeedAtTurn(params *DynamicsParams, containerMass, cogHeight, turnRadius float64) float64 {
	if turnRadius <= 0 {
		return DefaultMaxSpeed
	}

	totalMass := params.AGVTareMass + containerMass
	if totalMass <= 0 {
		return DefaultMaxSpeed
	}
	if cogHeight <= 0 {
		cogHeight = StandardContainerHeight / 2.0
	}

	staticStability := params.TrackWidth * totalMass * params.Gravity / 2.0
	dynamicMoment := totalMass * cogHeight * params.MaxCentripetalAccel

	if dynamicMoment >= staticStability {
		safeAccel := staticStability / (totalMass * cogHeight * turnRadius)
		return math.Sqrt(math.Max(0, safeAccel * turnRadius))
	}

	maxSpeed := math.Sqrt(params.MaxCentripetalAccel * turnRadius)
	return math.Min(maxSpeed, DefaultMaxSpeed)
}

type DegradedSpeedProfile struct {
	RiskLevel          RolloverRisk
	StraightMaxSpeed   float64
	TurnMaxSpeed       float64
	MaxCentripetalAccel float64
	SpeedReductionFactor float64
	IsDegraded         bool
	Reason             string
}

func CalculateDegradedProfile(ecc *Eccentricity, containerMass float64) *DegradedSpeedProfile {
	profile := &DegradedSpeedProfile{
		RiskLevel:          RiskNone,
		StraightMaxSpeed:   DefaultMaxSpeed,
		TurnMaxSpeed:       DefaultTurnSpeed,
		MaxCentripetalAccel: MaxCentripetalAccelNormal,
		SpeedReductionFactor: 1.0,
		IsDegraded:         false,
		Reason:             "normal",
	}

	if ecc == nil || !ecc.IsSevere {
		if ecc != nil && ecc.Magnitude >= EccentricityModerate {
			profile.RiskLevel = RiskLow
			profile.TurnMaxSpeed = DefaultTurnSpeed * 0.9
			profile.MaxCentripetalAccel = MaxCentripetalAccelModerate
			profile.SpeedReductionFactor = 0.9
			profile.Reason = "moderate_eccentricity"
		}
		return profile
	}

	cogHeight := StandardContainerHeight / 2.0
	effectiveHeight := cogHeight * (1.0 + ecc.Magnitude*0.5)

	switch {
	case ecc.IsCritical:
		profile.RiskLevel = RiskCritical
		profile.StraightMaxSpeed = DefaultMaxSpeed * 0.5
		profile.TurnMaxSpeed = 0.3
		profile.MaxCentripetalAccel = MaxCentripetalAccelCritical
		profile.SpeedReductionFactor = 0.3
		profile.IsDegraded = true
		profile.Reason = "critical_eccentricity_rollover_risk"

	case ecc.IsSevere:
		profile.RiskLevel = RiskHigh
		profile.StraightMaxSpeed = DefaultMaxSpeed * 0.7
		profile.TurnMaxSpeed = 0.6
		profile.MaxCentripetalAccel = MaxCentripetalAccelSevere
		profile.SpeedReductionFactor = 0.6
		profile.IsDegraded = true
		profile.Reason = "severe_eccentricity_reduced_cornering"

	default:
		profile.RiskLevel = RiskModerate
		profile.StraightMaxSpeed = DefaultMaxSpeed * 0.85
		profile.TurnMaxSpeed = DefaultTurnSpeed * 0.7
		profile.MaxCentripetalAccel = MaxCentripetalAccelModerate
		profile.SpeedReductionFactor = 0.75
		profile.IsDegraded = true
		profile.Reason = "moderate_eccentricity_caution"
	}

	_ = effectiveHeight
	return profile
}

func TurnRadiusFromAngleAndSpeed(deltaAngleDeg, speed float64) float64 {
	if math.Abs(deltaAngleDeg) < 5.0 || speed <= 0 {
		return math.Inf(1)
	}
	absAngle := math.Abs(deltaAngleDeg)
	radians := absAngle * math.Pi / 180.0
	wheelbase := DefaultWheelbase
	return wheelbase / math.Tan(radians/2.0)
}

func SafeCornerSpeed(profile *DegradedSpeedProfile, deltaAngleDeg float64) float64 {
	if profile == nil {
		return DefaultTurnSpeed
	}

	if math.Abs(deltaAngleDeg) < 5.0 {
		return profile.StraightMaxSpeed
	}

	turnRadius := TurnRadiusFromAngleAndSpeed(deltaAngleDeg, profile.TurnMaxSpeed)
	if math.IsInf(turnRadius, 1) {
		return profile.StraightMaxSpeed
	}

	params := DefaultDynamicsParams()
	params.MaxCentripetalAccel = profile.MaxCentripetalAccel

	safeSpeed := MaxSafeSpeedAtTurn(params, 0, StandardContainerHeight/2.0, turnRadius)
	return math.Min(safeSpeed, profile.TurnMaxSpeed)
}
