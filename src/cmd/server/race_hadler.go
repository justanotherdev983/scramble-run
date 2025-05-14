package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"
)

func homeHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title: "Scramble Run",
	}

	err := homeTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("raceHandler: Template execution error: %v", err)

	}
}

func raceHandler(w http.ResponseWriter, r *http.Request) {
	currentUserID := 1 // <<<< --- !!! !!! --- >>>
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

	raceMutex.Lock()
	pageNextRaceStartTime := nextRaceStartTime
	pageCurrentRaceDetails := currentRaceDetails // This is *RaceInfo
	raceMutex.Unlock()

	var calculatedTimeStr, calculatedStatusMsg, calculatedRaceName string
	isBettingInitiallyOpen := false
	var initialTrackRaceStatus string

	// Variables to hold the values for the new PageData fields
	isRaceActuallyFinished := false
	var actualWinnerID int // Assuming Chicken ID is int

	if pageCurrentRaceDetails != nil && pageCurrentRaceDetails.Status == RaceStatusRunning {
		calculatedStatusMsg = "Race in Progress:"
		calculatedRaceName = pageCurrentRaceDetails.Name
		calculatedTimeStr = "Running!"
		isBettingInitiallyOpen = false
		initialTrackRaceStatus = RaceStatusRunning
		// Race is running, not finished yet
		isRaceActuallyFinished = false
		actualWinnerID = 0 // No winner yet
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
			initialTrackRaceStatus = RaceStatusScheduled
		} else {
			calculatedTimeStr = "Starting..."
			calculatedStatusMsg = "Next race:"
			calculatedRaceName = "Get Ready!"
			isBettingInitiallyOpen = false
			initialTrackRaceStatus = RaceStatusScheduled
		}
		// Upcoming race, so previous race (if any) might be finished, but this block is for "next race"
		// We determine RaceFinished based on pageCurrentRaceDetails status if it's "Finished"
		if pageCurrentRaceDetails != nil && pageCurrentRaceDetails.Status == RaceStatusFinished {
			isRaceActuallyFinished = true
			// === How to get Winner ID? ===
			// Option A: If RaceInfo struct has WinnerChickenID
			// actualWinnerID = pageCurrentRaceDetails.WinnerChickenID

			// Option B: If RaceInfo.Winner is the NAME, and you need to look up the ID
			// from availableChickens or by another DB query (less ideal for handler).
			// For now, let's assume you have a way to get this ID. If not, the template logic needs adjustment.
			// If `pageCurrentRaceDetails.Winner` (string name) is reliable:
			if pageCurrentRaceDetails.Winner != "" {
				// Find the chicken in availableChickens that matches the winner name
				for _, chk := range availableChickens { // availableChickens should be populated by this point
					if chk.Name == pageCurrentRaceDetails.Winner {
						actualWinnerID = chk.ID // Assuming chk.ID is int
						break
					}
				}
				if actualWinnerID == 0 {
					log.Printf("homeHandler: Could not find ID for winner name '%s' in availableChickens", pageCurrentRaceDetails.Winner)
				}
			}
		}

	} else if pageCurrentRaceDetails != nil && pageCurrentRaceDetails.Status == RaceStatusFinished {
		calculatedStatusMsg = "Last race finished:"
		// pageCurrentRaceDetails.Winner is a string (name)
		calculatedRaceName = fmt.Sprintf("%s (Winner: %s)", pageCurrentRaceDetails.Name, pageCurrentRaceDetails.Winner)
		calculatedTimeStr = "Next one soon..."
		_, errActiveRace := getActiveRaceID(db)
		isBettingInitiallyOpen = errActiveRace == nil // Or perhaps false until next race countdown starts
		initialTrackRaceStatus = RaceStatusFinished

		// Race is finished
		isRaceActuallyFinished = true
		// === How to get Winner ID? (Same logic as above) ===
		if pageCurrentRaceDetails.Winner != "" {
			for _, chk := range availableChickens {
				if chk.Name == pageCurrentRaceDetails.Winner {
					actualWinnerID = chk.ID
					break
				}
			}
			if actualWinnerID == 0 {
				log.Printf("homeHandler: Could not find ID for winner name '%s' in availableChickens (finished block)", pageCurrentRaceDetails.Winner)
			}
		}

	} else { // No current race, no next race imminently, or error
		calculatedTimeStr = "--:--"
		calculatedStatusMsg = "Checking schedule..."
		_, errActiveRace := getActiveRaceID(db)
		if errActiveRace == nil {
			initialTrackRaceStatus = RaceStatusScheduled
			isBettingInitiallyOpen = true
		} else {
			initialTrackRaceStatus = RaceStatusNoRace
			isBettingInitiallyOpen = false
		}
		// No race active/finished to determine winner from
		isRaceActuallyFinished = false
		actualWinnerID = 0
	}

	var activeRaceForTemplate ActiveRace
	// Ensure availableChickens is populated *before* trying to find winner ID from it
	// availableChickens is used for ActiveRace.Chickens and for betting options
	// It should represent the chickens participating in the *current* or *next* race
	// This might be different from the chickens that participated in a *past* finished race.
	// For simplicity, the current availableChickens list is used to find the winner.
	// This assumes the winner of the last race is among the currently "available" chickens,
	// which might be true if they are persistent.
	activeRaceForTemplate = ActiveRace{Chickens: availableChickens}

	data := PageData{
		Title:                  "Scramble Run",
		UserData:               currentUser,
		UserBalance:            userBalance,
		Races:                  get_races(db),         // History
		Chickens:               availableChickens,     // For betting panel
		ActiveRace:             activeRaceForTemplate, // For track display
		PotentialWinnings:      0.0,
		InitialNextRaceTime:    calculatedTimeStr,
		InitialStatusMessage:   calculatedStatusMsg,
		InitialRaceName:        calculatedRaceName,
		IsBettingInitiallyOpen: isBettingInitiallyOpen,
		CurrentRaceDisplay:     pageCurrentRaceDetails, // Info about the just-finished/running race
		RaceStatus:             initialTrackRaceStatus, // For data-race-status on track

		// Populate the new fields
		RaceFinished: isRaceActuallyFinished,
		WinnerID:     actualWinnerID,
		// Message and Success are likely for form post responses, initialize if needed
		Message: "",
		Success: false,
	}

	err := raceTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("raceHandler: Template execution error: %v", err)

	}
}

// nextRaceInfoHandler provides HTMX updates for the race timer/status display.
func nextRaceInfoHandler(w http.ResponseWriter, r *http.Request) {
	// --- 1. Get User ID ---
	// This is CRUCIAL. You need a reliable way to get the current user's ID.
	// For now, I'll use a placeholder like in homeHandler, but this
	// MUST be replaced with your actual session/authentication logic.
	// Example: currentUserID := app.sessionManager.GetInt(r.Context(), "userID")
	var currentUserID int = 1 // <<<< --- !!! PLACEHOLDER: Replace with actual User ID from session/request context !!! --- >>>
	// If you have a session manager:
	// currentUserID = sessionManager.GetInt(r.Context(), "userID") // Assuming sessionManager is accessible

	var currentUserBalance float64
	userLoggedIn := false

	if currentUserID != 0 {
		// --- 2. Fetch User Balance Correctly ---
		errDb := db.QueryRow("SELECT balance FROM users WHERE id = ?", currentUserID).Scan(&currentUserBalance)
		if errDb != nil {
			if errDb == sql.ErrNoRows {
				log.Printf("nextRaceInfoHandler: User ID %d not found when fetching balance.", currentUserID)
				// currentUserBalance remains 0.0, userLoggedIn remains false
			} else {
				log.Printf("nextRaceInfoHandler: Error fetching balance for user ID %d: %v", currentUserID, errDb)
				// currentUserBalance remains 0.0, userLoggedIn remains false
			}
		} else {
			// Balance fetched successfully
			userLoggedIn = true
		}
	} else {
		log.Println("nextRaceInfoHandler: No user ID found (or user is guest), not fetching balance.")
		// currentUserBalance remains 0.0, userLoggedIn remains false
	}

	// --- Race Logic (copied from your existing code, assumed correct) ---
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
		countdownStr = "Running!"
	} else if !localNextRaceStartTime.IsZero() && localNextRaceStartTime.After(time.Now()) {
		durationUntilNext := time.Until(localNextRaceStartTime)
		if durationUntilNext > 10*time.Minute {
			log.Printf("nextRaceInfoHandler: Detected abnormally long time until next race: %v", durationUntilNext)
			countdownStr = "Soon™"
			statusMsg = "Next race:"
			raceNameDisplay = "Schedule being fixed..."
			go func() {
				if err := cleanupStaleScheduledRaces(db); err != nil {
					log.Printf("nextRaceInfoHandler: Background cleanup failed: %v", err)
				}
				if raceTicker != nil {
					raceTicker.Reset(100 * time.Millisecond)
				}
			}()
		} else if durationUntilNext > 0 {
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
		// Betting generally closed right after a race, check if a new one is immediately scheduled
		// _, err := getActiveRaceID(db)
		// isBettingOpen = (err == nil) // This might open betting too soon.
		isBettingOpen = false // More reliably, betting is closed until next race explicitly allows it.
	} else {
		countdownStr = "--:--"
		statusMsg = "Checking schedule..."
		raceNameDisplay = "No active race"
		isBettingOpen = false // Default to closed
	}
	// --- End of Race Logic ---

	// --- 3. Populate Data Struct ---
	data := struct {
		CountdownStr       string
		StatusMsg          string
		RaceName           string
		IsBettingOpen      bool
		IsRaceRunning      bool
		UserLoggedIn       bool    // Now correctly determined
		CurrentUserBalance float64 // Now correctly fetched
	}{
		CountdownStr:       countdownStr,
		StatusMsg:          statusMsg,
		RaceName:           raceNameDisplay,
		IsBettingOpen:      isBettingOpen,
		IsRaceRunning:      isRaceRunning,
		UserLoggedIn:       userLoggedIn,       // Use the determined value
		CurrentUserBalance: currentUserBalance, // Use the fetched value
	}

	w.Header().Set("Content-Type", "text/html")
	// Ensure raceInfoTemplate is parsed and includes the OOB swap for balance
	if raceInfoTemplate == nil {
		log.Fatal("nextRaceInfoHandler: raceInfoTemplate is nil!") // Should be initialized at startup
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
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
