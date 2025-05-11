package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"
)

func homeHandler(w http.ResponseWriter, r *http.Request) {
	currentUserID := 1 // <<<< --- !!! PLACEHOLDER: Replace with actual User ID from session !!! --- >>>
	var currentUser User
	var userBalance float64

	if currentUserID != 0 {
		errDb := db.QueryRow("SELECT id, name, email, balance FROM users WHERE id = ?", currentUserID).Scan(&currentUser.ID, &currentUser.Name, &currentUser.Email, &userBalance)
		if errDb != nil {
			if errDb == sql.ErrNoRows {
				log.Printf("homeHandler: User ID %d not found. Displaying as Guest.", currentUserID)
				currentUser.Name = "Guest"
				userBalance = 0
			} else {
				log.Printf("homeHandler: Error fetching user data for ID %d: %v", currentUserID, errDb)
				currentUser.Name = "Error"
				userBalance = 0
			}
		}
	} else {
		currentUser.Name = "Guest"
		userBalance = 0
	}

	data := PageData{
		Title:             "Scramble Run",
		UserData:          currentUser,
		UserBalance:       userBalance,
		Races:             get_races(db),
		Chickens:          availableChickens,
		ActiveRace:        ActiveRace{Chickens: availableChickens},
		PotentialWinnings: 0.0,
	}

	raceMutex.Lock()
	pageNextRaceStartTime := nextRaceStartTime
	pageCurrentRaceDetails := currentRaceDetails
	raceMutex.Unlock()

	var calculatedTimeStr, calculatedStatusMsg, calculatedRaceName string
	isBettingInitiallyOpen := false

	if pageCurrentRaceDetails != nil && pageCurrentRaceDetails.Status == RaceStatusRunning {
		calculatedStatusMsg = "Race in Progress:"
		calculatedRaceName = pageCurrentRaceDetails.Name
		calculatedTimeStr = "Running!"
		isBettingInitiallyOpen = false
	} else if !pageNextRaceStartTime.IsZero() && pageNextRaceStartTime.After(time.Now()) {
		durationUntilNext := time.Until(pageNextRaceStartTime)
		if durationUntilNext > 0 {
			minutes := int(durationUntilNext.Minutes())
			seconds := int(durationUntilNext.Seconds()) % 60
			calculatedTimeStr = fmt.Sprintf("%02d:%02d", minutes, seconds)
			calculatedStatusMsg = "Next race in:"
			var nextRaceNameDB string
			errDb := db.QueryRow("SELECT name FROM races WHERE status = ? AND date = ? ORDER BY date ASC LIMIT 1",
				RaceStatusScheduled, pageNextRaceStartTime).Scan(&nextRaceNameDB)
			if errDb == nil {
				calculatedRaceName = nextRaceNameDB
			}
			isBettingInitiallyOpen = true
		} else {
			calculatedTimeStr = "Starting..."
			calculatedStatusMsg = "Next race:"
			calculatedRaceName = "Get Ready!"
			isBettingInitiallyOpen = false
		}
	} else if pageCurrentRaceDetails != nil && pageCurrentRaceDetails.Status == RaceStatusFinished {
		calculatedStatusMsg = "Last race finished:"
		calculatedRaceName = fmt.Sprintf("%s (Winner: %s)", pageCurrentRaceDetails.Name, pageCurrentRaceDetails.Winner)
		calculatedTimeStr = "Next one soon..."
		_, errActiveRace := getActiveRaceID(db)
		isBettingInitiallyOpen = (errActiveRace == nil)
	} else {
		calculatedTimeStr = "--:--"
		calculatedStatusMsg = "Checking schedule..."
		_, errActiveRace := getActiveRaceID(db)
		isBettingInitiallyOpen = (errActiveRace == nil)
	}

	data.InitialNextRaceTime = calculatedTimeStr
	data.InitialStatusMessage = calculatedStatusMsg
	data.InitialRaceName = calculatedRaceName
	data.IsBettingInitiallyOpen = isBettingInitiallyOpen
	data.CurrentRaceDisplay = pageCurrentRaceDetails

	err := homeTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("homeHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// nextRaceInfoHandler provides HTMX updates for the race timer/status display.
func nextRaceInfoHandler(w http.ResponseWriter, r *http.Request) {
	raceMutex.Lock()
	localNextRaceStartTime := nextRaceStartTime
	localCurrentRaceDetails := currentRaceDetails
	raceMutex.Unlock()

	var countdownStr, statusMsg, raceNameDisplay string
	isBettingOpen := false
	isRaceRunning := false

	if localCurrentRaceDetails != nil && localCurrentRaceDetails.Status == RaceStatusRunning {
		statusMsg = "Race in Progress:"
		raceNameDisplay = localCurrentRaceDetails.Name
		isRaceRunning = true
		isBettingOpen = false
	} else if !localNextRaceStartTime.IsZero() && localNextRaceStartTime.After(time.Now()) {
		durationUntilNext := time.Until(localNextRaceStartTime)
		if durationUntilNext > 0 {
			minutes := int(durationUntilNext.Minutes())
			seconds := int(durationUntilNext.Seconds()) % 60
			countdownStr = fmt.Sprintf("%02d:%02d", minutes, seconds)
			statusMsg = "Next race starts in:"
			var nextRaceNameDB string
			err := db.QueryRow("SELECT name FROM races WHERE status = ? AND date = ? ORDER BY date ASC LIMIT 1",
				RaceStatusScheduled, localNextRaceStartTime).Scan(&nextRaceNameDB)
			if err == nil {
				raceNameDisplay = nextRaceNameDB
			} else {
				raceNameDisplay = "Upcoming Race"
			}
			isBettingOpen = true
		} else {
			countdownStr = "Starting..."
			statusMsg = "Next race:"
			raceNameDisplay = "Get Ready!"
			isBettingOpen = false
		}
	} else if localCurrentRaceDetails != nil && localCurrentRaceDetails.Status == RaceStatusFinished {
		statusMsg = "Last race finished:"
		raceNameDisplay = fmt.Sprintf("%s (Winner: %s)", localCurrentRaceDetails.Name, localCurrentRaceDetails.Winner)
		countdownStr = "Next one soon..."
		_, err := getActiveRaceID(db)
		isBettingOpen = (err == nil)
	} else {
		countdownStr = "--:--"
		statusMsg = "Checking schedule..."
		raceNameDisplay = "No active race"
		_, err := getActiveRaceID(db)
		isBettingOpen = (err == nil)
	}

	data := struct {
		CountdownStr  string
		StatusMsg     string
		RaceName      string
		IsBettingOpen bool
		IsRaceRunning bool
	}{
		CountdownStr:  countdownStr,
		StatusMsg:     statusMsg,
		RaceName:      raceNameDisplay,
		IsBettingOpen: isBettingOpen,
		IsRaceRunning: isRaceRunning,
	}

	w.Header().Set("Content-Type", "text/html")
	err := raceInfoTemplate.Execute(w, data)
	if err != nil {
		log.Printf("nextRaceInfoHandler: Error executing template: %v", err)
	}
}

// handleTriggerRaceCycle is an admin endpoint to manually advance the race lifecycle.
func handleTriggerRaceCycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	log.Println("ADMIN: Manual trigger for race cycle received.")

	raceMutex.Lock()
	currentRace := currentRaceDetails
	raceMutex.Unlock()

	forcedAction := "No specific action forced, triggering scheduler."

	if currentRace != nil && currentRace.Status == RaceStatusRunning {
		log.Printf("ADMIN: Forcing finish for currently running race ID %d: %s", currentRace.Id, currentRace.Name)
		if raceEndTimer != nil {
			raceEndTimer.Stop()
		}
		err := finishRace(db, currentRace.Id)
		if err != nil {
			log.Printf("ADMIN: Error force-finishing race %d: %v", currentRace.Id, err)
			forcedAction = fmt.Sprintf("Error force-finishing race %d: %v", currentRace.Id, err)
		} else {
			log.Printf("ADMIN: Race %d force-finished.", currentRace.Id)
			forcedAction = fmt.Sprintf("Race %d force-finished.", currentRace.Id)
		}
	} else {
		var raceToStartID int
		var raceToStartName, dateStrToStart string

		err := db.QueryRow("SELECT id, name, date FROM races WHERE status = ? ORDER BY date ASC LIMIT 1",
			RaceStatusScheduled).Scan(&raceToStartID, &raceToStartName, &dateStrToStart)

		if err == nil {
			log.Printf("ADMIN: Forcing start for next scheduled race ID %d: %s", raceToStartID, raceToStartName)
			errStart := startRace(db, raceToStartID)
			if errStart != nil {
				log.Printf("ADMIN: Error force-starting race %d: %v", raceToStartID, errStart)
				forcedAction = fmt.Sprintf("Error force-starting race %d: %v", raceToStartID, errStart)
			} else {
				forcedAction = fmt.Sprintf("Race %d (%s) force-started.", raceToStartID, raceToStartName)
			}
		} else if err == sql.ErrNoRows {
			log.Println("ADMIN: No race running and no race scheduled to force-start.")
			forcedAction = "No race running or scheduled to force."
			scheduled, sErr := scheduleNewRace(db)
			if sErr != nil {
				forcedAction += " Error scheduling new: " + sErr.Error()
			}
			if scheduled {
				forcedAction += " New race scheduled."
			}
		} else {
			log.Printf("ADMIN: Error finding race to force-start: %v", err)
			forcedAction = "Error finding race to force: " + err.Error()
		}
	}

	if raceTicker != nil {
		log.Println("ADMIN: Resetting raceTicker to re-evaluate scheduling post-trigger.")
		raceTicker.Reset(100 * time.Millisecond)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Race cycle triggered. Action: %s. Check server logs.\n", forcedAction)
	log.Printf("ADMIN: Manual race cycle trigger processed. Action: %s", forcedAction)
}