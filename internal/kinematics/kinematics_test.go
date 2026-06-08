package kinematics

import (
	"math"
	"testing"

	"github.com/port-agv/routing/internal/router"
)

func TestCalculateCOGBalanced(t *testing.T) {
	cw := CornerWeights{FrontLeft: 5000, FrontRight: 5000, RearLeft: 5000, RearRight: 5000}
	cog := CalculateCOG(cw, StandardContainerLength, StandardContainerWidth)

	if math.Abs(cog.XOffset) > 0.01 {
		t.Fatalf("expected XOffset≈0 for balanced load, got %.4f", cog.XOffset)
	}
	if math.Abs(cog.YOffset) > 0.01 {
		t.Fatalf("expected YOffset≈0 for balanced load, got %.4f", cog.YOffset)
	}
}

func TestCalculateCOGFrontHeavy(t *testing.T) {
	cw := CornerWeights{FrontLeft: 8000, FrontRight: 8000, RearLeft: 2000, RearRight: 2000}
	cog := CalculateCOG(cw, StandardContainerLength, StandardContainerWidth)

	if cog.XOffset <= 0 {
		t.Fatalf("expected positive XOffset (front-heavy), got %.4f", cog.XOffset)
	}
	if cog.XRatio <= 0 {
		t.Fatalf("expected positive XRatio (front-heavy), got %.4f", cog.XRatio)
	}
}

func TestCalculateCOGRightHeavy(t *testing.T) {
	cw := CornerWeights{FrontLeft: 2000, FrontRight: 8000, RearLeft: 2000, RearRight: 8000}
	cog := CalculateCOG(cw, StandardContainerLength, StandardContainerWidth)

	if cog.YOffset <= 0 {
		t.Fatalf("expected positive YOffset (right-heavy), got %.4f", cog.YOffset)
	}
}

func TestEccentricityBalanced(t *testing.T) {
	cw := CornerWeights{FrontLeft: 5000, FrontRight: 5000, RearLeft: 5000, RearRight: 5000}
	profile := NewContainerLoadProfile("TEST-01", cw)
	ecc := profile.Eccentricity

	if ecc.IsSevere {
		t.Fatal("balanced load should not be severe")
	}
	if ecc.Magnitude > EccentricityModerate {
		t.Fatalf("balanced load eccentricity should be low, got %.3f", ecc.Magnitude)
	}
}

func TestEccentricitySevere(t *testing.T) {
	cw := CornerWeights{FrontLeft: 15000, FrontRight: 15000, RearLeft: 500, RearRight: 500}
	profile := NewContainerLoadProfile("TEST-02", cw)
	ecc := profile.Eccentricity

	if !ecc.IsSevere {
		t.Fatal("heavily unbalanced load should be severe")
	}
	if ecc.Direction != "front" {
		t.Fatalf("expected front direction, got %s", ecc.Direction)
	}
}

func TestEccentricityCritical(t *testing.T) {
	cw := CornerWeights{FrontLeft: 20000, FrontRight: 20000, RearLeft: 100, RearRight: 100}
	profile := NewContainerLoadProfile("TEST-03", cw)
	ecc := profile.Eccentricity

	if !ecc.IsCritical {
		t.Fatalf("extremely unbalanced load should be critical, magnitude=%.3f", ecc.Magnitude)
	}
}

func TestContainerLoadProfileInvalid(t *testing.T) {
	cw := CornerWeights{FrontLeft: 0, FrontRight: 0, RearLeft: 0, RearRight: 0}
	profile := NewContainerLoadProfile("TEST-00", cw)
	if profile.IsValid {
		t.Fatal("zero-weight container should be invalid")
	}
}

func TestRolloverThresholdSpeed(t *testing.T) {
	speed := RolloverThresholdSpeed(2.0, 1.5, 9.81, 10.0)
	if speed <= 0 {
		t.Fatal("expected positive threshold speed")
	}
	t.Logf("Rollover threshold: trackWidth=2.0 cogHeight=1.5 radius=10.0 → speed=%.2f m/s", speed)
}

func TestRolloverThresholdNarrowerTrack(t *testing.T) {
	speed1 := RolloverThresholdSpeed(2.0, 1.5, 9.81, 10.0)
	speed2 := RolloverThresholdSpeed(1.5, 1.5, 9.81, 10.0)
	if speed2 >= speed1 {
		t.Fatal("narrower track width should give lower threshold speed")
	}
}

func TestDegradedProfileNormal(t *testing.T) {
	ecc := &Eccentricity{Magnitude: 0.05, IsSevere: false, IsCritical: false}
	profile := CalculateDegradedProfile(ecc, 20000)
	if profile.IsDegraded {
		t.Fatal("low eccentricity should not cause degradation")
	}
	if profile.StraightMaxSpeed != DefaultMaxSpeed {
		t.Fatalf("expected max speed %.1f, got %.1f", DefaultMaxSpeed, profile.StraightMaxSpeed)
	}
}

func TestDegradedProfileSevere(t *testing.T) {
	ecc := &Eccentricity{Magnitude: 0.25, IsSevere: true, IsCritical: false, Direction: "front"}
	profile := CalculateDegradedProfile(ecc, 20000)
	if !profile.IsDegraded {
		t.Fatal("severe eccentricity should cause degradation")
	}
	if profile.TurnMaxSpeed >= DefaultTurnSpeed {
		t.Fatal("degraded turn speed should be lower than default")
	}
	if profile.StraightMaxSpeed >= DefaultMaxSpeed {
		t.Fatal("degraded straight speed should be lower than default")
	}
}

func TestDegradedProfileCritical(t *testing.T) {
	ecc := &Eccentricity{Magnitude: 0.40, IsSevere: true, IsCritical: true, Direction: "right"}
	profile := CalculateDegradedProfile(ecc, 20000)
	if !profile.IsDegraded {
		t.Fatal("critical eccentricity should cause degradation")
	}
	if profile.SpeedReductionFactor > 0.4 {
		t.Fatalf("critical reduction factor should be very low, got %.2f", profile.SpeedReductionFactor)
	}
}

func TestSafetyGatewayInterceptNoData(t *testing.T) {
	sg := NewSafetyGateway()
	result := sg.Intercept("UNKNOWN-CONTAINER")
	if !result.Allowed {
		t.Fatal("should allow dispatch when no sensor data available")
	}
	if result.DegradedSpeeds != nil && result.DegradedSpeeds.IsDegraded {
		t.Fatal("should not degrade without data")
	}
}

func TestSafetyGatewayInterceptWithBalancedData(t *testing.T) {
	sg := NewSafetyGateway()
	cw := CornerWeights{FrontLeft: 8000, FrontRight: 8000, RearLeft: 8000, RearRight: 8000}
	sg.RegisterContainerLoad("C-BAL", cw)

	result := sg.Intercept("C-BAL")
	if !result.Allowed {
		t.Fatal("balanced container should be allowed")
	}
	if result.DegradedSpeeds != nil && result.DegradedSpeeds.IsDegraded {
		t.Fatal("balanced container should not trigger degradation")
	}
}

func TestSafetyGatewayInterceptWithSevereData(t *testing.T) {
	sg := NewSafetyGateway()
	cw := CornerWeights{FrontLeft: 18000, FrontRight: 18000, RearLeft: 500, RearRight: 500}
	sg.RegisterContainerLoad("C-UNBAL", cw)

	result := sg.Intercept("C-UNBAL")
	if !result.Allowed {
		t.Fatal("severe container should still be allowed (with degradation)")
	}
	if result.DegradedSpeeds == nil || !result.DegradedSpeeds.IsDegraded {
		t.Fatal("severe container should trigger speed degradation")
	}
	if result.DegradedSpeeds.TurnMaxSpeed >= DefaultTurnSpeed {
		t.Fatal("degraded turn speed should be lower than default")
	}
}

func TestApplySpeedDegradation(t *testing.T) {
	frames := []router.ControlFrame{
		{Sequence: 0, Maneuver: router.ManeuverStart, Speed: 0.5, NodeID: "A"},
		{Sequence: 1, Maneuver: router.ManeuverStraight, Speed: 6.0, NodeID: "B"},
		{Sequence: 2, Maneuver: router.ManeuverTurnRight, Speed: 1.5, DeltaAngle: 45, NodeID: "C"},
		{Sequence: 3, Maneuver: router.ManeuverUTurn, Speed: 1.5, DeltaAngle: 180, NodeID: "D"},
		{Sequence: 4, Maneuver: router.ManeuverStop, Speed: 0, NodeID: "E"},
	}

	sg := NewSafetyGateway()
	profile := &DegradedSpeedProfile{
		RiskLevel:          RiskHigh,
		StraightMaxSpeed:   4.0,
		TurnMaxSpeed:       0.6,
		MaxCentripetalAccel: MaxCentripetalAccelSevere,
		SpeedReductionFactor: 0.6,
		IsDegraded:         true,
		Reason:             "severe_eccentricity",
	}

	degraded := sg.ApplySpeedDegradation(frames, profile)

	if degraded[1].Speed > 4.0 {
		t.Fatalf("straight speed should be capped at 4.0, got %.1f", degraded[1].Speed)
	}
	if degraded[2].Speed > 0.6 {
		t.Fatalf("turn speed should be capped at 0.6, got %.1f", degraded[2].Speed)
	}

	if frames[1].Speed != 6.0 {
		t.Fatal("original frames should not be modified")
	}
}

func TestApplySpeedDegradationNoOp(t *testing.T) {
	frames := []router.ControlFrame{
		{Sequence: 0, Maneuver: router.ManeuverStraight, Speed: 6.0, NodeID: "A"},
	}
	sg := NewSafetyGateway()
	profile := &DegradedSpeedProfile{IsDegraded: false}

	result := sg.ApplySpeedDegradation(frames, profile)
	if result[0].Speed != 6.0 {
		t.Fatal("non-degraded profile should not change speeds")
	}
}

func TestProcessCraneSensorMessage(t *testing.T) {
	sg := NewSafetyGateway()
	payload := `{"crane_id":"QC-01","container_id":"C-SENSOR","weights":{"front_left":15000,"front_right":15000,"rear_left":500,"rear_right":500},"timestamp":1700000000}`

	sg.ProcessCraneSensorMessage([]byte(payload))

	profile := sg.GetContainerProfile("C-SENSOR")
	if profile == nil {
		t.Fatal("container should be registered after sensor message")
	}
	if !profile.IsValid {
		t.Fatal("container should be valid")
	}
	if !profile.Eccentricity.IsSevere {
		t.Fatal("heavily unbalanced sensor data should be severe")
	}
}

func TestTurnRadiusCalculation(t *testing.T) {
	r1 := TurnRadiusFromAngleAndSpeed(90, 3.0)
	if r1 <= 0 {
		t.Fatal("expected positive turn radius for 90-degree turn")
	}

	r2 := TurnRadiusFromAngleAndSpeed(3, 3.0)
	if !math.IsInf(r2, 1) {
		t.Fatal("expected infinite radius for near-straight (3-degree)")
	}

	r3 := TurnRadiusFromAngleAndSpeed(180, 3.0)
	if r3 >= r1 {
		t.Fatal("180-degree turn should have smaller radius than 90-degree")
	}
}

func TestCraneSensorData(t *testing.T) {
	sg := NewSafetyGateway()
	data := &CraneSensorData{
		CraneID:     "QC-01",
		ContainerID: "C-001",
		Weights:     CornerWeights{FrontLeft: 8000, FrontRight: 8000, RearLeft: 8000, RearRight: 8000},
	}
	sg.RegisterCraneSensor(data)

	profile := sg.GetContainerProfile("C-001")
	if profile == nil || !profile.IsValid {
		t.Fatal("crane sensor data should register a valid profile")
	}
}
