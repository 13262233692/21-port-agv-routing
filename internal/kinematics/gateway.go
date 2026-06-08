package kinematics

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/port-agv/routing/internal/router"
)

type InterceptionResult struct {
	Allowed        bool
	Profile        *ContainerLoadProfile
	DegradedSpeeds *DegradedSpeedProfile
	Reason         string
}

type SafetyGateway struct {
	mu            sync.RWMutex
	containerData map[string]*ContainerLoadProfile
	cargoMasses   map[string]float64
}

func NewSafetyGateway() *SafetyGateway {
	return &SafetyGateway{
		containerData: make(map[string]*ContainerLoadProfile),
		cargoMasses:   make(map[string]float64),
	}
}

func (sg *SafetyGateway) RegisterContainerLoad(containerID string, cw CornerWeights) *ContainerLoadProfile {
	profile := NewContainerLoadProfile(containerID, cw)

	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.containerData[containerID] = profile

	if profile.IsValid {
		cargoMass := profile.PayloadWeight()
		sg.cargoMasses[containerID] = cargoMass
		log.Printf("[SafetyGateway] Container %s: total=%.0fkg cargo=%.0fkg eccentricity=%.3f (%s) severe=%v critical=%v",
			containerID, profile.TotalWeight, cargoMass,
			profile.Eccentricity.Magnitude, profile.Eccentricity.Direction,
			profile.Eccentricity.IsSevere, profile.Eccentricity.IsCritical)
	}

	return profile
}

func (sg *SafetyGateway) RegisterCraneSensor(data *CraneSensorData) *ContainerLoadProfile {
	return sg.RegisterContainerLoad(data.ContainerID, data.Weights)
}

func (sg *SafetyGateway) GetContainerProfile(containerID string) *ContainerLoadProfile {
	sg.mu.RLock()
	defer sg.mu.RUnlock()
	return sg.containerData[containerID]
}

func (sg *SafetyGateway) Intercept(containerID string) *InterceptionResult {
	sg.mu.RLock()
	profile, ok := sg.containerData[containerID]
	sg.mu.RUnlock()

	if !ok || profile == nil || !profile.IsValid {
		return &InterceptionResult{
			Allowed: true,
			Reason:  "no_sensor_data_using_default_params",
		}
	}

	cargoMass := profile.PayloadWeight()
	degraded := CalculateDegradedProfile(profile.Eccentricity, cargoMass)

	result := &InterceptionResult{
		Allowed:        true,
		Profile:        profile,
		DegradedSpeeds: degraded,
		Reason:         degraded.Reason,
	}

	if degraded.RiskLevel == RiskCritical && profile.Eccentricity.Magnitude >= EccentricityCritical {
		result.Allowed = true
		result.Reason = "critical_eccentricity_dispatched_with_max_degradation"
	}

	log.Printf("[SafetyGateway] Intercept container=%s: allowed=%v risk=%d degraded=%v speed_factor=%.2f reason=%s",
		containerID, result.Allowed, degraded.RiskLevel, degraded.IsDegraded,
		degraded.SpeedReductionFactor, result.Reason)

	return result
}

func (sg *SafetyGateway) ApplySpeedDegradation(frames []router.ControlFrame, profile *DegradedSpeedProfile) []router.ControlFrame {
	if profile == nil || !profile.IsDegraded {
		return frames
	}

	result := make([]router.ControlFrame, len(frames))
	copy(result, frames)

	for i := range result {
		switch result[i].Maneuver {
		case router.ManeuverTurnLeft, router.ManeuverTurnRight, router.ManeuverUTurn:
			safeSpeed := SafeCornerSpeed(profile, result[i].DeltaAngle)
			result[i].Speed = safeSpeed
			if result[i].Speed > profile.TurnMaxSpeed {
				result[i].Speed = profile.TurnMaxSpeed
			}

		case router.ManeuverStraight, router.ManeuverAccelerate:
			if result[i].Speed > profile.StraightMaxSpeed {
				result[i].Speed = profile.StraightMaxSpeed
			}

		case router.ManeuverStart:
			if result[i].Speed > profile.TurnMaxSpeed {
				result[i].Speed = profile.TurnMaxSpeed
			}
		}
	}

	return result
}

func (sg *SafetyGateway) ProcessCraneSensorMessage(payload []byte) {
	var data CraneSensorData
	if err := json.Unmarshal(payload, &data); err != nil {
		log.Printf("[SafetyGateway] Invalid crane sensor payload: %v", err)
		return
	}
	sg.RegisterCraneSensor(&data)
}
