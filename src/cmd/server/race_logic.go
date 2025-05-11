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

	var existingRaceCount int
	err := db.QueryRow("SELECT COUNT(*) FROM races WHERE status = ? OR status = ?", RaceStatusScheduled, RaceStatusRunning).Scan(&existingRaceCount)
	if err != nil {
		log.Printf("scheduleNewRace: Error checking for existing races: %v", err)
		return false, err
	}

	if existingRaceCount > 0 {
		var status string
		var dateStr string
		err = db.QueryRow("SELECT status, date FROM races WHERE status = ? OR status = ? ORDER BY date ASC LIMIT 1", RaceStatusScheduled, RaceStatusRunning).Scan(&status, &dateStr)
		if err == nil {
			parsedTime, _ := parseRaceDate(dateStr)
			if status == RaceStatusScheduled {
				nextRaceStartTime = parsedTime
				currentRaceDetails = nil
				log.Printf("scheduleNewRace: A race is already scheduled for %v. No new race created.", nextRaceStartTime)
			} else { // RaceStatusRunning
				if currentRaceDetails == nil || currentRaceDetails.Status != RaceStatusRunning {
					var raceID int
					db.QueryRow("SELECT id FROM races WHERE status = ? ORDER BY date ASC LIMIT 1", RaceStatusRunning).Scan(&raceID)
					if raceID > 0 {
						currentRaceDetails, _ = getRaceDetails(db, raceID)
					}
				}
				log.Printf("scheduleNewRace: A race is currently running. No new race created.")
			}
		}
		return false, nil
	}

	scheduledTime := time.Now().Add(raceInterval)
	raceName := generateRaceName()

	result, err := db.Exec("INSERT INTO races (name, date, status) VALUES (?, ?, ?)", raceName, scheduledTime, RaceStatusScheduled)
	if err != nil {
		log.Printf("scheduleNewRace: Error inserting new race: %v", err)
		return false, err
	}
	newRaceID64, _ := result.LastInsertId()
	nextRaceStartTime = scheduledTime
	currentRaceDetails = nil

	log.Printf("Scheduled new race: ID %d, Name: '%s', StartTime: %v", newRaceID64, raceName, scheduledTime)
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
			payout = betAmount + winnings // Original bet + profit
			
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
func raceLoop(db *sql.DB) {
	isRaceSystemActive = true
	log.Println("Race Manager: Starting race loop...")

	_, err := scheduleNewRace(db)
	if err != nil {
		log.Printf("Race Manager: Initial race scheduling/check failed: %v. Will retry via ticker.", err)
	}

	raceTicker = time.NewTicker(5 * time.Second)
	defer raceTicker.Stop()
	if raceEndTimer != nil {
		raceEndTimer.Stop()
	}

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
				continue
			}

			if !rnNextRaceStartTime.IsZero() && time.Now().After(rnNextRaceStartTime) {
				var raceToStartID int
				var raceNameToStart string
				err := db.QueryRow("SELECT id, name FROM races WHERE status = ? AND date <= ? ORDER BY date ASC LIMIT 1",
					RaceStatusScheduled, time.Now()).Scan(&raceToStartID, &raceNameToStart)

				if err == nil {
					log.Printf("Race Manager: Time to start race ID %d ('%s'). Starting now.", raceToStartID, raceNameToStart)
					errStart := startRace(db, raceToStartID)
					if errStart != nil {
						log.Printf("Race Manager: Failed to start race %d: %v.", raceToStartID, errStart)
						raceMutex.Lock()
						nextRaceStartTime = time.Time{}
						raceMutex.Unlock()
					}
					continue
				} else if err != sql.ErrNoRows {
					log.Printf("Race Manager: Error finding scheduled race to start: %v", err)
				}
			}

			if (rnCurrentRace == nil || rnCurrentRace.Status == RaceStatusFinished) || rnNextRaceStartTime.IsZero() {
				scheduled, scheduleErr := scheduleNewRace(db)
				if scheduleErr != nil {
					log.Printf("Race Manager: Error during periodic scheduling: %v", scheduleErr)
				} else if !scheduled {
					// log.Printf("Race Manager: scheduleNewRace decided not to schedule a new race this tick.")
				}
			}
		}
	}
}