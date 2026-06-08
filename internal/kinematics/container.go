package kinematics

import (
	"math"
)

const (
	StandardContainerLength = 12.192
	StandardContainerWidth  = 2.438
	StandardContainerHeight = 2.591
	StandardContainerTare   = 2250.0
	MaxPayload20ft          = 28250.0
)

type CornerWeights struct {
	FrontLeft  float64 `json:"front_left"`
	FrontRight float64 `json:"front_right"`
	RearLeft   float64 `json:"rear_left"`
	RearRight  float64 `json:"rear_right"`
}

type CenterOfGravity struct {
	XOffset float64
	YOffset float64
	XRatio  float64
	YRatio  float64
}

type Eccentricity struct {
	Direction      string  `json:"direction"`
	Magnitude      float64 `json:"magnitude"`
	IsSevere       bool    `json:"is_severe"`
	IsCritical     bool    `json:"is_critical"`
	LateralRatio   float64 `json:"lateral_ratio"`
	LongitudinalRatio float64 `json:"longitudinal_ratio"`
}

const (
	EccentricityModerate = 0.10
	EccentricitySevere   = 0.20
	EccentricityCritical = 0.35
)

func CalculateCOG(w CornerWeights, containerLength, containerWidth float64) *CenterOfGravity {
	totalWeight := w.FrontLeft + w.FrontRight + w.RearLeft + w.RearRight
	if totalWeight <= 0 {
		return &CenterOfGravity{}
	}

	halfL := containerLength / 2.0
	halfW := containerWidth / 2.0

	xMoment := (w.FrontLeft+w.FrontRight)*halfL - (w.RearLeft+w.RearRight)*halfL
	yMoment := (w.FrontRight+w.RearRight)*halfW - (w.FrontLeft+w.RearLeft)*halfW

	xOffset := xMoment / totalWeight
	yOffset := yMoment / totalWeight

	var xRatio, yRatio float64
	if halfL > 0 {
		xRatio = xOffset / halfL
	}
	if halfW > 0 {
		yRatio = yOffset / halfW
	}

	return &CenterOfGravity{
		XOffset: xOffset,
		YOffset: yOffset,
		XRatio:  xRatio,
		YRatio:  yRatio,
	}
}

func CalculateEccentricity(cog *CenterOfGravity) *Eccentricity {
	if cog == nil {
		return &Eccentricity{}
	}

	longAbs := math.Abs(cog.XRatio)
	latAbs := math.Abs(cog.YRatio)
	magnitude := math.Sqrt(cog.XRatio*cog.XRatio + cog.YRatio*cog.YRatio)

	dir := ""
	if longAbs >= latAbs {
		if cog.XRatio > 0 {
			dir = "front"
		} else if cog.XRatio < 0 {
			dir = "rear"
		}
	} else {
		if cog.YRatio > 0 {
			dir = "right"
		} else if cog.YRatio < 0 {
			dir = "left"
		}
	}

	return &Eccentricity{
		Direction:         dir,
		Magnitude:         magnitude,
		IsSevere:          magnitude >= EccentricitySevere,
		IsCritical:        magnitude >= EccentricityCritical,
		LateralRatio:      latAbs,
		LongitudinalRatio: longAbs,
	}
}

type ContainerLoadProfile struct {
	ContainerID  string
	CornerWeights CornerWeights
	TotalWeight  float64
	COG          *CenterOfGravity
	Eccentricity *Eccentricity
	TareWeight   float64
	IsValid      bool
}

func NewContainerLoadProfile(containerID string, cw CornerWeights) *ContainerLoadProfile {
	total := cw.FrontLeft + cw.FrontRight + cw.RearLeft + cw.RearRight
	if total <= 0 {
		return &ContainerLoadProfile{
			ContainerID:   containerID,
			CornerWeights: cw,
			IsValid:       false,
		}
	}

	cog := CalculateCOG(cw, StandardContainerLength, StandardContainerWidth)
	ecc := CalculateEccentricity(cog)

	return &ContainerLoadProfile{
		ContainerID:   containerID,
		CornerWeights: cw,
		TotalWeight:   total,
		COG:           cog,
		Eccentricity:  ecc,
		TareWeight:    StandardContainerTare,
		IsValid:       true,
	}
}

func (p *ContainerLoadProfile) PayloadWeight() float64 {
	return math.Max(0, p.TotalWeight-p.TareWeight)
}

func (p *ContainerLoadProfile) LoadFactor() float64 {
	if MaxPayload20ft <= 0 {
		return 0
	}
	return p.PayloadWeight() / MaxPayload20ft
}

type CraneSensorData struct {
	CraneID     string        `json:"crane_id"`
	ContainerID string        `json:"container_id"`
	Weights     CornerWeights `json:"weights"`
	Timestamp   int64         `json:"timestamp"`
}
