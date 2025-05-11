package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"time"
)

// generateRaceName creates a whimsical name for a race.
func generateRaceName() string {
	adjectives := []string{"Speedy", "Thunder", "Golden", "Lightning", "Cosmic", "農場 (Farm)", "Feathered", "Clucky"}
	nouns := []string{"Derby", "Sprint", "Classic", "Gallop", "Frenzy", "Run", "Cup", "Challenge"}
	return fmt.Sprintf("%s %s #%d", adjectives[rand.Intn(len(adjectives))], nouns[rand.Intn(len(nouns))], rand.Intn(1000))
}

// scheduleNewRace attempts to schedule a new race if no active (Scheduled or Running) race exists.
func scheduleNewRace(db *sql.DB) (bool, error) {
	raceMutex.Lock()
	defer raceMutex.Unlock()

	// First, check and clean up any stale scheduled races
	if err := cleanupStaleScheduledRaces(db); err != nil {
		log.Printf("scheduleNewRace: Error during stale race cleanup: %v", err)
		// Continue anyway - not fatal
	}

	// Rest of the function remains mostly unchanged...
	var staleRaceID int
	// Check specifically for any race that might be stuck in 'Running' state
	errStale := db.QueryRow("SELECT id FROM races WHERE status = ? ORDER BY date ASC LIMIT 1", RaceStatusRunning).Scan(&staleRaceID)
	if errStale == nil {
		// Found a race in 'Running' state. Assume it's stale from a previous session.
		log.Printf("scheduleNewRace: Found stale race ID %d in 'Running' state. Marking as Finished.", staleRaceID)
		_, errUpdateStale := db.Exec("UPDATE races SET status = ?, winner_chicken_id = NULL WHERE id = ? AND status = ?", RaceStatusFinished, staleRaceID, RaceStatusRunning)
		if errUpdateStale != nil {
			log.Printf("scheduleNewRace: Error marking stale running race ID %d as Finished: %v", staleRaceID, errUpdateStale)
		} else {
			log.Printf("scheduleNewRace: Stale running race ID %d successfully marked as Finished.", staleRaceID)
		}
		if currentRaceDetails != nil && currentRaceDetails.Id == staleRaceID {
			currentRaceDetails.Status = RaceStatusFinished
			currentRaceDetails.Winner = "N/A (Stale)"
			currentRaceDetails.WinnerChickenID = sql.NullInt64{Valid: false}
		}
	} else if errStale != sql.ErrNoRows {
		log.Printf("scheduleNewRace: Error checking for stale running races: %v", errStale)
		return false, errStale
	}

	// Now, check for an existing 'Scheduled' race or if a 'Running' race still exists
	var existingRaceCount int
	err := db.QueryRow("SELECT COUNT(*) FROM races WHERE status = ? OR status = ?", RaceStatusScheduled, RaceStatusRunning).Scan(&existingRaceCount)
	if err != nil {
		log.Printf("scheduleNewRace: Error checking for existing races after stale check: %v", err)
		return false, err
	}

	if existingRaceCount > 0 {
		var status string
		var dateStr string
		var raceID int
		// Get the earliest scheduled or running race
		err = db.QueryRow("SELECT id, status, date FROM races WHERE status = ? OR status = ? ORDER BY date ASC LIMIT 1", RaceStatusScheduled, RaceStatusRunning).Scan(&raceID, &status, &dateStr)
		if err == nil {
			parsedTime, pErr := parseRaceDate(dateStr)
			if pErr != nil {
				log.Printf("scheduleNewRace: Error parsing date '%s' for race ID %d: %v", dateStr, raceID, pErr)
				return false, pErr
			}

			// IMPORTANT FIX: Check if the parsedTime is unreasonably far in the future
			if status == RaceStatusScheduled && time.Until(parsedTime) > 10*time.Minute {
				log.Printf("scheduleNewRace: Found a race scheduled too far in the future (%v). Rescheduling it.", parsedTime)
				// Delete this race and continue to schedule a new one
				_, delErr := db.Exec("DELETE FROM races WHERE id = ? AND status = ?", raceID, RaceStatusScheduled)
				if delErr != nil {
					log.Printf("scheduleNewRace: Error deleting far-future race: %v", delErr)
					// Continue anyway
				}
			} else if status == RaceStatusScheduled {
				nextRaceStartTime = parsedTime
				currentRaceDetails = nil
				log.Printf("scheduleNewRace: A race (ID %d) is already scheduled for %v (%v from now). No new race created.",
					raceID, nextRaceStartTime, time.Until(nextRaceStartTime))
				return false, nil
			} else { // RaceStatusRunning
				if currentRaceDetails == nil || currentRaceDetails.Id != raceID || currentRaceDetails.Status != RaceStatusRunning {
					fetchedRaceDetails, rdErr := getRaceDetails(db, raceID)
					if rdErr == nil {
						currentRaceDetails = fetchedRaceDetails
					} else {
						log.Printf("scheduleNewRace: Could not fetch details for running race %d: %v. Using minimal info.", raceID, rdErr)
						currentRaceDetails = &RaceInfo{Id: raceID, Status: RaceStatusRunning, Name: "Race " + strconv.Itoa(raceID)}
					}
				}
				nextRaceStartTime = time.Time{}
				log.Printf("scheduleNewRace: A race (ID %d) is currently running. No new race created.", raceID)
				return false, nil
			}
		} else {
			log.Printf("scheduleNewRace: Error fetching details of existing race: %v", err)
		}
	}

	// IMPORTANT FIX: Ensure raceInterval is reasonable
	// Change this interval to be much shorter for testing/debugging
	raceInterval := 30 * time.Second // Use short intervals initially, can increase once fixed

	// If no 'Scheduled' or 'Running' races exist, schedule a new one.
	scheduledTime := time.Now().Add(raceInterval)
	raceName := generateRaceName()

	log.Printf("scheduleNewRace: Creating new race '%s' scheduled for %v (%v from now)",
		raceName, scheduledTime, time.Until(scheduledTime))

	result, err := db.Exec("INSERT INTO races (name, date, status) VALUES (?, ?, ?)",
		raceName, scheduledTime.Format(time.RFC3339), RaceStatusScheduled)
	if err != nil {
		log.Printf("scheduleNewRace: Error inserting new race: %v", err)
		return false, err
	}
	newRaceID64, _ := result.LastInsertId()
	nextRaceStartTime = scheduledTime
	currentRaceDetails = nil

	log.Printf("Scheduled new race: ID %d, Name: '%s', StartTime: %v",
		newRaceID64, raceName, scheduledTime)
	return true, nil
}

// startRace marks a scheduled race as 'Running' and sets up its end timer.
func startRace(db *sql.DB, raceID int) error {
	raceMutex.Lock()

	log.Printf("Attempting to start race ID: %d", raceID)
	res, err := db.Exec("UPDATE races SET status = ? WHERE id = ? AND status = ?", RaceStatusRunning, raceID, RaceStatusScheduled)
	if err != nil {
		raceMutex.Unlock()
		log.Printf("startRace: Error updating race %d to Running: %v", raceID, err)
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		raceMutex.Unlock()
		log.Printf("startRace: Race ID %d not found in 'Scheduled' state or already started.", raceID)
		var currentStatus string
		db.QueryRow("SELECT status FROM races WHERE id = ?", raceID).Scan(&currentStatus)
		log.Printf("startRace: Current status of race %d is '%s'", raceID, currentStatus)
		return fmt.Errorf("race %d not in 'Scheduled' state", raceID)
	}

	raceInfo, err := getRaceDetails(db, raceID)
	if err != nil {
		log.Printf("startRace: Could not fetch details for running race %d: %v. Using minimal info.", raceID, err)
		currentRaceDetails = &RaceInfo{Id: raceID, Status: RaceStatusRunning, Name: "Race " + strconv.Itoa(raceID)}
	} else {
		currentRaceDetails = raceInfo
	}
	if currentRaceDetails != nil {
		currentRaceDetails.Status = RaceStatusRunning
	}

	log.Printf("Race ID: %d (%s) started. Will finish in %v.", raceID, currentRaceDetails.Name, raceDuration)
	nextRaceStartTime = time.Time{}
	raceMutex.Unlock()

	if raceEndTimer != nil {
		raceEndTimer.Stop()
	}
	raceEndTimer = time.AfterFunc(raceDuration, func() {
		log.Printf("Race end timer fired for race ID: %d", raceID)
		err := finishRace(db, raceID)
		if err != nil {
			log.Printf("Error auto-finishing race %d: %v", raceID, err)
		}
	})
	return nil
}

// finishRace marks a running race as 'Finished', determines a winner, and settles bets.
func finishRace(db *sql.DB, raceID int) error {
	raceMutex.Lock()

	log.Printf("Attempting to finish race ID: %d", raceID)

	var currentStatus string
	err := db.QueryRow("SELECT status FROM races WHERE id = ?", raceID).Scan(&currentStatus)
	if err != nil {
		raceMutex.Unlock()
		if err == sql.ErrNoRows {
			log.Printf("finishRace: Race ID %d not found.", raceID)
			return fmt.Errorf("race %d not found", raceID)
		}
		log.Printf("finishRace: Error querying status for race %d: %v", raceID, err)
		return err
	}

	if currentStatus != RaceStatusRunning {
		raceMutex.Unlock()
		log.Printf("finishRace: Race ID %d is not 'Running' (status: %s). Cannot finish.", raceID, currentStatus)
		if currentStatus == RaceStatusFinished {
			return nil
		}
		return fmt.Errorf("race %d is not 'Running', status is %s", raceID, currentStatus)
	}

	if len(availableChickens) == 0 {
		log.Printf("finishRace: No available chickens to determine a winner for race %d.", raceID)
		_, errDb := db.Exec("UPDATE races SET status = ?, winner_chicken_id = NULL WHERE id = ?", RaceStatusFinished, raceID)
		if errDb != nil {
			log.Printf("finishRace: Error updating race %d to Finished with no winner: %v", raceID, errDb)
		}
		if currentRaceDetails != nil && currentRaceDetails.Id == raceID {
			currentRaceDetails.Status = RaceStatusFinished
			currentRaceDetails.Winner = "N/A (No chickens)"
			currentRaceDetails.WinnerChickenID = sql.NullInt64{Valid: false}
		}
		raceMutex.Unlock()
		return errDb
	}

	winnerChicken := availableChickens[rand.Intn(len(availableChickens))]
	log.Printf("Race ID: %d finished. Winner: %s (ID: %d)", raceID, winnerChicken.Name, winnerChicken.ID)

	tx, errTx := db.Begin()
	if errTx != nil {
		raceMutex.Unlock()
		log.Printf("finishRace: Failed to begin transaction for race %d: %v", raceID, errTx)
		return errTx
	}
	committed := false
	defer func() {
		if !committed {
			errRollback := tx.Rollback()
			if errRollback != nil {
				log.Printf("finishRace: Error rolling back transaction for race %d: %v", raceID, errRollback)
			} else {
				log.Printf("finishRace: Transaction for race %d rolled back.", raceID)
			}
		}
	}()

	_, err = tx.Exec("UPDATE races SET status = ?, winner_chicken_id = ? WHERE id = ?", RaceStatusFinished, winnerChicken.ID, raceID)
	if err != nil {
		raceMutex.Unlock()
		log.Printf("finishRace: Error updating race %d to Finished in DB: %v", raceID, err)
		return err
	}

	errSettle := settleBetsForRace(tx, raceID, winnerChicken.ID)
	if errSettle != nil {
		raceMutex.Unlock()
		log.Printf("finishRace: Error settling bets for race %d: %v", raceID, errSettle)
		return errSettle
	}

	errCommit := tx.Commit()
	if errCommit != nil {
		raceMutex.Unlock()
		log.Printf("finishRace: Error committing transaction for race %d: %v", raceID, errCommit)
		return errCommit
	}
	committed = true

	if currentRaceDetails != nil && currentRaceDetails.Id == raceID {
		currentRaceDetails.Status = RaceStatusFinished
		currentRaceDetails.Winner = winnerChicken.Name
		currentRaceDetails.WinnerChickenID = sql.NullInt64{Int64: int64(winnerChicken.ID), Valid: true}
	} else {
		updatedRaceInfo, grErr := getRaceDetails(db, raceID)
		if grErr == nil && updatedRaceInfo != nil {
			currentRaceDetails = updatedRaceInfo
		} else if grErr != nil {
			log.Printf("finishRace: Error fetching updated race details for race %d: %v", raceID, grErr)
		}
	}
	raceMutex.Unlock()

	log.Printf("Race %d successfully marked as Finished. Winner: %s. Bets settled. The raceLoop will schedule.", raceID, winnerChicken.Name)
	return nil
}

// settleBetsForRace processes 'Pending' bets for a finished race.
func settleBetsForRace(tx *sql.Tx, raceID int, winningChickenID int) error {
	log.Printf("Settling bets for Race ID: %d, Winning Chicken ID: %d", raceID, winningChickenID)

	var wonStatusID, lostStatusID int
	err := tx.QueryRow("SELECT id FROM bet_statuses WHERE status_name = 'Won'").Scan(&wonStatusID)
	if err != nil {
		return fmt.Errorf("could not find 'Won' bet status ID: %w", err)
	}
	err = tx.QueryRow("SELECT id FROM bet_statuses WHERE status_name = 'Lost'").Scan(&lostStatusID)
	if err != nil {
		return fmt.Errorf("could not find 'Lost' bet status ID: %w", err)
	}
	pendingStatusID, err := getPendingBetStatusID(tx)
	if err != nil {
		return fmt.Errorf("could not find 'Pending' bet status ID for settling: %w", err)
	}

	rows, err := tx.Query(`
        SELECT b.id, b.user_id, b.chicken_id, b.bet_amount, c.odds
        FROM bets b
        JOIN chickens c ON b.chicken_id = c.id
        WHERE b.race_id = ? AND b.bet_status_id = ?
    `, raceID, pendingStatusID)
	if err != nil {
		return fmt.Errorf("error querying pending bets for race %d: %w", raceID, err)
	}
	defer rows.Close()

	var betsProcessedCount int
	for rows.Next() {
		betsProcessedCount++
		var betID, userID, betChickenID int
		var betAmount, chickenOdds float64
		if err := rows.Scan(&betID, &userID, &betChickenID, &betAmount, &chickenOdds); err != nil {
			log.Printf("settleBetsForRace: Error scanning bet row for race %d: %v", raceID, err)
			continue
		}

		var payout float64 = 0
		newStatusID := lostStatusID

		if betChickenID == winningChickenID {
			// Calculate total payout: original bet + winnings
			winnings := betAmount * (chickenOdds - 1) // Just the profit
			payout = betAmount + winnings             // Original bet + profit

			newStatusID = wonStatusID
			log.Printf("Bet ID %d (User %d) on chicken %d WON. Bet: %.2f, Odds: %.2f, Payout: %.2f (returning bet + %.2f winnings)",
				betID, userID, betChickenID, betAmount, chickenOdds, payout, winnings)

			// Update user balance with total payout (bet + winnings)
			result, errUpdateBalance := tx.Exec("UPDATE users SET balance = balance + ? WHERE id = ?", payout, userID)
			if errUpdateBalance != nil {
				log.Printf("settleBetsForRace: Failed to update balance for user %d after winning bet %d: %v", userID, betID, errUpdateBalance)
				return fmt.Errorf("failed to update balance for user %d on win: %w", userID, errUpdateBalance)
			}

			// Verify the update actually affected a row
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				log.Printf("settleBetsForRace: WARNING - Update balance query for user %d (bet %d) affected 0 rows", userID, betID)
			}
		} else {
			log.Printf("Bet ID %d (User %d) on chicken %d LOST. Winning chicken was %d.",
				betID, userID, betChickenID, winningChickenID)
		}

		_, errUpdateBet := tx.Exec("UPDATE bets SET bet_status_id = ?, actual_payout = ? WHERE id = ?", newStatusID, payout, betID)
		if errUpdateBet != nil {
			log.Printf("settleBetsForRace: Failed to update status for bet %d: %v", betID, errUpdateBet)
			return fmt.Errorf("failed to update status for bet %d: %w", betID, errUpdateBet)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating bet rows for race %d: %w", raceID, err)
	}

	if betsProcessedCount == 0 {
		log.Printf("settleBetsForRace: No pending bets found for race ID %d to process.", raceID)
	} else {
		log.Printf("settleBetsForRace: Processed %d pending bets for race %d.", betsProcessedCount, raceID)
	}
	return nil
}

// raceLoop is the main goroutine for managing the race lifecycle.
// raceLoop is the main goroutine for managing the race lifecycle.
func raceLoop(db *sql.DB) {
	isRaceSystemActive = true // Assuming this is a global controlling the loop
	log.Println("Race Manager: Starting race loop...")

	// Initial scheduling attempt
	_, err := scheduleNewRace(db)
	if err != nil {
		log.Printf("Race Manager: Initial race scheduling/check failed: %v. Will retry via ticker.", err)
	}

	raceTicker = time.NewTicker(5 * time.Second) // Assuming raceTicker is global or package-level
	defer raceTicker.Stop()
	// if raceEndTimer != nil { // Assuming raceEndTimer is global or package-level
	// 	raceEndTimer.Stop()
	// }

	for {
		if !isRaceSystemActive {
			log.Println("Race Manager: Shutting down race loop.")
			return
		}

		select {
		case <-raceTicker.C:
			raceMutex.Lock()
			rnNextRaceStartTime := nextRaceStartTime
			rnCurrentRace := currentRaceDetails
			raceMutex.Unlock()

			if rnCurrentRace != nil && rnCurrentRace.Status == RaceStatusRunning {
				// log.Printf("Race Manager: Race ID %d is currently running. Skipping checks.", rnCurrentRace.Id)
				continue
			}

			// Check if it's time to start a scheduled race
			if !rnNextRaceStartTime.IsZero() && time.Now().After(rnNextRaceStartTime) {
				var raceToStartID int
				var raceNameToStart string

				// More detailed logging for the query
				currentTimeForQuery := time.Now() // Get current time once for this block
				currentTimeFormatted := currentTimeForQuery.Format(time.RFC3339)
				queryStr := "SELECT id, name FROM races WHERE status = ? AND date <= ? ORDER BY date ASC LIMIT 1"

				log.Printf("Race Manager: Attempting to find race to start. rnNextRaceStartTime: %v. Current time: %v. Query: races.date <= '%s'",
					rnNextRaceStartTime, currentTimeForQuery, currentTimeFormatted)

				errFindRace := db.QueryRow(queryStr, RaceStatusScheduled, currentTimeFormatted).Scan(&raceToStartID, &raceNameToStart)

				if errFindRace == nil {
					log.Printf("Race Manager: Found race to start: ID %d ('%s'). Starting now.", raceToStartID, raceNameToStart)
					errStart := startRace(db, raceToStartID)
					if errStart != nil {
						log.Printf("Race Manager: Failed to start race %d: %v. Resetting nextRaceStartTime.", raceToStartID, errStart)
						raceMutex.Lock()
						nextRaceStartTime = time.Time{} // Reset to allow rescheduling
						raceMutex.Unlock()
					}
					// Whether startRace succeeded or failed, we've processed this time slot.
					// currentRaceDetails and nextRaceStartTime will be updated by startRace or reset on failure.
					continue // Proceed to the next ticker cycle
				} else if errFindRace == sql.ErrNoRows {
					log.Printf("Race Manager: No due scheduled race found in DB (sql.ErrNoRows). Searched with date <= '%s'. rnNextRaceStartTime was %v. Fallthrough to scheduleNewRace.",
						currentTimeFormatted, rnNextRaceStartTime)
					// No 'continue' here, so it will fall through to potentially schedule a new race
				} else { // Other database error
					log.Printf("Race Manager: DB error finding scheduled race to start: %v. Searched with date <= '%s'. Fallthrough to scheduleNewRace.",
						errFindRace, currentTimeFormatted)
					// No 'continue' here
				}
			}

			// If no race is running, and (either no race is scheduled OR it wasn't time to start one yet OR finding/starting failed)
			// then try to schedule a new one.
			// The previous block handles starting due races. If it fell through, it means no race was started.
			if rnCurrentRace == nil || rnCurrentRace.Status == RaceStatusFinished {
				// If rnNextRaceStartTime is set but the race failed to start (e.g. ErrNoRows from query above),
				// we might want to clear rnNextRaceStartTime before calling scheduleNewRace
				// to ensure it doesn't just find the same "stuck" race again.
				// However, scheduleNewRace has its own logic to find the earliest.
				// The key is that the above block *should* have started the race if it was findable and due.
				// If it gets here, it implies either no race is scheduled, or the scheduled one isn't due,
				// or the due one couldn't be started (and nextRaceStartTime might have been reset if startRace failed hard).

				// Let's re-evaluate the condition to call scheduleNewRace:
				// We call scheduleNewRace if:
				// 1. No race is running AND
				// 2. EITHER no next race is known (nextRaceStartTime is Zero)
				//    OR the known next race is still in the future (not time to start it yet)
				//    OR (implicitly) the attempt to start a due race just failed and fell through.

				// If a race is supposed to be running (currentRaceDetails not nil and Running), we already continued.
				// If nextRaceStartTime is set and in the past, the block above should have handled it or logged an error.
				// If it fell through, it means that logic concluded.
				// So, now we check if we *need* to schedule.
				// We schedule if no race is active (Scheduled or Running).
				// scheduleNewRace itself checks if a race is already scheduled or running.

				// The original condition: (rnCurrentRace == nil || rnCurrentRace.Status == RaceStatusFinished) || rnNextRaceStartTime.IsZero()
				// If rnNextRaceStartTime is PAST, the above block should handle it. If that block leads to ErrNoRows,
				// then scheduleNewRace will be called.
				// This seems to be the current behavior.
				// The critical part is making sure that if a race *should* start, the previous block *does* start it.

				// More precise condition to call scheduleNewRace:
				// Call if no race is currently running AND (no next race is scheduled OR the next scheduled race is not the one we just tried to start)
				// This gets complex. The original logic in scheduleNewRace to check existing races is probably sufficient.
				// The main issue is the query in raceLoop not finding the race.

				// The fall-through is causing scheduleNewRace to be called.
				// The problem remains: why did the query for raceToStartID yield ErrNoRows?
				// If that's fixed, scheduleNewRace won't be called when it shouldn't.

				// log.Printf("Race Manager: Condition for scheduleNewRace: currentRace status: %v, nextRaceStartTime: %v. Calling scheduleNewRace.", rnCurrentRace.Status, rnNextRaceStartTime)
				scheduled, scheduleErr := scheduleNewRace(db) // This will re-log if it finds race 71 again
				if scheduleErr != nil {
					log.Printf("Race Manager: Error during periodic scheduling by raceLoop: %v", scheduleErr)
				} else if !scheduled {
					// log.Printf("Race Manager: scheduleNewRace decided not to schedule a new race this tick (from raceLoop).")
				}
			}
		}
	}
}

func cleanupStaleScheduledRaces(db *sql.DB) error {
	// Define a reasonable max future time (e.g., races shouldn't be scheduled more than 1 hour out)
	cleanupThresholdDuration := 2000 * time.Hour
	maxAcceptableFutureTime := time.Now().Add(cleanupThresholdDuration)

	// Find any scheduled races that are too far in the future
	rows, err := db.Query("SELECT id, name, date FROM races WHERE status = ? AND date > ?",
		RaceStatusScheduled, maxAcceptableFutureTime.Format(time.RFC3339))
	if err != nil {
		log.Printf("cleanupStaleScheduledRaces: Error querying for stale scheduled races (threshold: >%v from now): %v", cleanupThresholdDuration, err)
		return err
	}
	defer rows.Close()

	staleCount := 0
	for rows.Next() {
		var raceID int
		var raceName, dateStr string
		if err := rows.Scan(&raceID, &raceName, &dateStr); err != nil {
			log.Printf("cleanupStaleScheduledRaces: Error scanning race row: %v", err)
			continue // Skip this row, try next
		}

		// Log details about the race being cleaned up.
		// parseRaceDate is assumed to correctly parse the dateStr from the DB.
		// If parseRaceDate is not available or part of your provided code, ensure it handles RFC3339 strings.
		parsedTime, pErr := parseRaceDate(dateStr) // Ensure parseRaceDate is defined, e.g., time.Parse(time.RFC3339, dateStr)

		logMessagePrefix := fmt.Sprintf("cleanupStaleScheduledRaces: Race ID %d ('%s')", raceID, raceName)

		if pErr != nil {
			log.Printf("%s: Error parsing date '%s': %v. Query selected it as stale (scheduled beyond %v from now). Proceeding with removal.",
				logMessagePrefix, dateStr, pErr, cleanupThresholdDuration)
		} else {
			log.Printf("%s: Found scheduled too far in the future. Scheduled for: %v (%v from now). Threshold is >%v from now. Removing.",
				logMessagePrefix, parsedTime, time.Until(parsedTime), cleanupThresholdDuration)
		}

		_, delErr := db.Exec("DELETE FROM races WHERE id = ? AND status = ?", raceID, RaceStatusScheduled)
		if delErr != nil {
			log.Printf("%s: Error deleting stale race: %v", logMessagePrefix, delErr)
			// Potentially return error here or continue to try cleaning others
		} else {
			log.Printf("%s: Successfully removed.", logMessagePrefix)
			staleCount++
		}
	}

	if err := rows.Err(); err != nil { // Check for errors encountered during iteration
		log.Printf("cleanupStaleScheduledRaces: Error after iterating rows: %v", err)
		return err // Return this error as it might indicate a problem with the result set
	}

	if staleCount > 0 {
		log.Printf("cleanupStaleScheduledRaces: Finished cleanup. Removed %d stale scheduled races (older than %v from now).", staleCount, cleanupThresholdDuration)
	}
	// Optional: log when no stale races are found for verbosity
	// else {
	//  log.Printf("cleanupStaleScheduledRaces: No scheduled races found beyond %v from now.", cleanupThresholdDuration)
	// }

	return nil
}
