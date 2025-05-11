package main

import (
	_ "encoding/json"
	_ "log"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ChickenPosition represents a chicken's position in the race
type ChickenPosition struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Color    string  `json:"color"`
	Lane     int     `json:"lane"`
	Progress float64 `json:"progress"`
	IsWinner bool    `json:"isWinner"`
}

// RaceAnimationState tracks the state of the current race animation
type RaceAnimationState struct {
	RaceID        int               `json:"raceId"`
	RaceName      string            `json:"raceName"`
	IsRunning     bool              `json:"isRunning"`
	StartTime     time.Time         `json:"startTime"`
	EndTime       time.Time         `json:"endTime"`
	WinnerID      int               `json:"winnerId"`
	Chickens      []ChickenPosition `json:"chickens"`
	ProgressMutex sync.Mutex
}

// Global race animation state
var (
	currentRaceAnimation *RaceAnimationState
	raceAnimationMutex   sync.Mutex
)

// initRaceAnimation initializes a new race animation when a race starts
func initRaceAnimation(raceID int, raceName string) {
	raceAnimationMutex.Lock()
	defer raceAnimationMutex.Unlock()

	// Create chicken positions based on available chickens
	chickenPositions := make([]ChickenPosition, len(availableChickens))
	for i, chicken := range availableChickens {
		chickenPositions[i] = ChickenPosition{
			ID:       chicken.ID,
			Name:     chicken.Name,
			Color:    chicken.Color,
			Lane:     chicken.Lane,
			Progress: 0,
			IsWinner: false,
		}
	}

	// Initialize race animation state
	currentRaceAnimation = &RaceAnimationState{
		RaceID:    raceID,
		RaceName:  raceName,
		IsRunning: true,
		StartTime: time.Now(),
		EndTime:   time.Now().Add(raceDuration),
		WinnerID:  0,
		Chickens:  chickenPositions,
	}

	// Start the animation update goroutine
	go updateRaceAnimation()
}

// updateRaceAnimation periodically updates chicken positions during a race
func updateRaceAnimation() {
	// Update positions every 100ms
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			raceAnimationMutex.Lock()
			if currentRaceAnimation == nil || !currentRaceAnimation.IsRunning {
				raceAnimationMutex.Unlock()
				return
			}

			// Calculate race progress percentage (0-100%)
			totalDuration := currentRaceAnimation.EndTime.Sub(currentRaceAnimation.StartTime)
			elapsed := time.Since(currentRaceAnimation.StartTime)
			raceProgress := float64(elapsed) / float64(totalDuration)

			// Check if race is finished
			if raceProgress >= 1.0 {
				currentRaceAnimation.IsRunning = false
				raceAnimationMutex.Unlock()
				return
			}

			// Update each chicken's position
			currentRaceAnimation.ProgressMutex.Lock()
			for i := range currentRaceAnimation.Chickens {
				// Each chicken has a slightly different speed
				speedFactor := 0.8 + (rand.Float64() * 0.4) // 0.8-1.2 speed factor

				// Calculate new progress based on race progress and chicken's speed
				newProgress := raceProgress * 90 * speedFactor // Max progress is 90%

				// Ensure progress doesn't exceed 90% (finish line)
				if newProgress > 90 {
					newProgress = 90
				}

				currentRaceAnimation.Chickens[i].Progress = newProgress
			}
			currentRaceAnimation.ProgressMutex.Unlock()
			raceAnimationMutex.Unlock()
		}
	}
}

// finishRaceAnimation marks the race as finished and sets the winner
func finishRaceAnimation(winnerID int) {
	raceAnimationMutex.Lock()
	defer raceAnimationMutex.Unlock()

	if currentRaceAnimation == nil {
		return
	}

	currentRaceAnimation.IsRunning = false
	currentRaceAnimation.WinnerID = winnerID

	// Mark the winner
	for i := range currentRaceAnimation.Chickens {
		if currentRaceAnimation.Chickens[i].ID == winnerID {
			currentRaceAnimation.Chickens[i].IsWinner = true
			// Ensure winner is at the finish line
			currentRaceAnimation.Chickens[i].Progress = 90
		}
	}
}

// raceUpdateHandler provides real-time updates on chicken positions during a race
func raceUpdateHandler(w http.ResponseWriter, r *http.Request) {
	raceAnimationMutex.Lock()
	defer raceAnimationMutex.Unlock()

	w.Header().Set("Content-Type", "text/html")

	// If no race is running, show placeholder
	if currentRaceAnimation == nil {
		raceMutex.Lock()
		raceDetails := currentRaceDetails
		raceMutex.Unlock()

		if raceDetails != nil && raceDetails.Status == RaceStatusRunning {
			// Race is running but animation not initialized - initialize it
			initRaceAnimation(raceDetails.Id, raceDetails.Name)
		} else {
			// No race running
			w.Write([]byte(`<div class="race-placeholder">Waiting for next race to start...</div>`))
			return
		}
	}

	// If we have a race animation, render the chickens
	if currentRaceAnimation != nil {
		currentRaceAnimation.ProgressMutex.Lock()

		// Generate HTML for each chicken
		html := ""
		for _, chicken := range currentRaceAnimation.Chickens {
			winnerAttr := ""
			winnerClass := ""
			winnerCrown := ""

			if chicken.IsWinner {
				winnerAttr = `data-winner="true"`
				winnerClass = `class="chicken winner"`
				winnerCrown = `<div class="winner-crown">👑</div>`
			} else {
				winnerClass = `class="chicken"`
			}

			html += `<div id="chicken-` + string(chicken.ID) + `" ` + winnerClass + ` ` + winnerAttr + ` style="top: ` + string(chicken.Lane) + `%; left: ` + strconv.FormatFloat(chicken.Progress, 'f', -1, 64) + `%; transition: left 0.5s ease-in-out;">
				` + winnerCrown + `
				<div class="chicken-body" style="background-color: ` + chicken.Color + `"></div>
				<div class="chicken-wing"></div>
				<div class="chicken-beak"></div>
				<span class="chicken-name">` + chicken.Name + `</span>
			</div>`
		}

		// Add track lanes
		html += `
			<div class="track-lane" style="top: 20%"></div>
			<div class="track-lane" style="top: 40%"></div>
			<div class="track-lane" style="top: 60%"></div>
			<div class="track-lane" style="top: 80%"></div>
		`

		currentRaceAnimation.ProgressMutex.Unlock()
		w.Write([]byte(html))
	}
}
