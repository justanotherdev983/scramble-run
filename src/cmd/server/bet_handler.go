package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// selectChickenHandler handles requests to select a chicken and show potential winnings.
func selectChickenHandler(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 3 {
		http.Error(w, "Invalid URL: Missing chicken ID", http.StatusBadRequest)
		return
	}

	chickenIDStr := pathParts[len(pathParts)-1]
	chickenID, err := strconv.Atoi(chickenIDStr)
	if err != nil {
		http.Error(w, "Invalid chicken ID format", http.StatusBadRequest)
		return
	}

	var selectedChicken Chicken
	found := false
	for _, chicken := range availableChickens {
		if chicken.ID == chickenID {
			selectedChicken = chicken
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Chicken not found", http.StatusNotFound)
		return
	}

	betAmount := 10.0 // Default bet amount
	betAmountStr := r.URL.Query().Get("betAmount")
	if betAmountStr != "" {
		parsedAmount, parseErr := strconv.ParseFloat(betAmountStr, 64)
		if parseErr == nil && parsedAmount > 0 {
			betAmount = parsedAmount
		}
	}

	potentialWinnings := betAmount * selectedChicken.Odds

	w.Header().Set("Content-Type", "text/html")
	tmpl := template.Must(template.New("winningsCalc").Parse(`
        <div class="winnings-display" id="winnings-calc">
            <p>Potential Win:</p>
            <span class="winnings-amount">{{printf "%.2f" .Amount}} Credits</span>
            <input type="hidden" name="selectedChicken" value="{{.ChickenID}}" />
        </div>
    `))

	winningsData := WinningsCalc{
		Amount:    potentialWinnings,
		ChickenID: chickenID,
	}

	err = tmpl.Execute(w, winningsData)
	if err != nil {
		log.Printf("selectChickenHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// calculateWinningsHandler re-calculates potential winnings based on user input.
func calculateWinningsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		log.Printf("calculateWinningsHandler: Failed to parse form: %v", err)
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	betAmountStr := r.Form.Get("betAmount")
	betAmount, err := strconv.ParseFloat(betAmountStr, 64)
	if err != nil || betAmount <= 0 {
		http.Error(w, "Invalid bet amount", http.StatusBadRequest)
		return
	}

	chickenIDStr := r.Form.Get("selectedChicken")
	chickenID, err := strconv.Atoi(chickenIDStr)
	if err != nil {
		http.Error(w, "Invalid chicken ID", http.StatusBadRequest)
		return
	}

	var selectedChickenOdds float64
	found := false
	for _, chicken := range availableChickens {
		if chicken.ID == chickenID {
			selectedChickenOdds = chicken.Odds
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Chicken not found", http.StatusNotFound)
		return
	}

	winningsData := WinningsCalc{
		Amount:    betAmount * selectedChickenOdds,
		ChickenID: chickenID,
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl := template.Must(template.New("winningsCalcResponse").Parse(`
        <div class="winnings-display" id="winnings-calc">
            <p>Potential Win:</p>
            <span class="winnings-amount">{{printf "%.2f" .Amount}} Credits</span>
            <input type="hidden" name="selectedChicken" value="{{.ChickenID}}" />
        </div>
    `))

	err = tmpl.Execute(w, winningsData)
	if err != nil {
		log.Printf("calculateWinningsHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// placeBetHandler handles the submission of a bet.
func placeBetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	// --- Authentication ---
	if sessionManager == nil {
		log.Printf("placeBetHandler: CRITICAL: sessionManager is not initialized.")
		// Attempt to send a structured error if the template is available
		if betResponseTemplate != nil {
			_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Server configuration error. Please try again later.", NewBalance: -1})
		} else {
			http.Error(w, "Server configuration error", http.StatusInternalServerError)
		}
		return
	}

	currentUserID := sessionManager.GetInt(r.Context(), sessionUserIDKey)
	var userCurrentBalanceForErrorDisplay float64 = -1
	// Fetch initial balance for error display if needed, outside transaction for non-critical info
	// This is a bit redundant as we fetch it again in TX, but okay for display purposes.
	if currentUserID != 0 {
		// Best effort, don't fail hard here if this query fails
		_ = db.QueryRow("SELECT balance FROM users WHERE id = ?", currentUserID).Scan(&userCurrentBalanceForErrorDisplay)
	}

	err := r.ParseForm()
	if err != nil {
		log.Printf("placeBetHandler: Failed to parse form: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Error processing request.", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}

	betAmountStr := r.FormValue("betAmount")
	betAmount, err := strconv.ParseFloat(betAmountStr, 64)
	if err != nil || betAmount <= 0 {
		log.Printf("placeBetHandler: Invalid bet amount. String: '%s', Parsed: %f, Error: %v", betAmountStr, betAmount, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Invalid bet amount. Must be a positive number.", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}

	chickenIDStr := r.FormValue("selectedChicken")
	if chickenIDStr == "" {
		log.Println("placeBetHandler: 'selectedChicken' form value is empty.")
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "No chicken selected. Please select a chicken first.", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}
	chickenID, err := strconv.Atoi(chickenIDStr)
	if err != nil {
		log.Printf("placeBetHandler: Failed to convert 'selectedChicken' value '%s' to an integer: %v", chickenIDStr, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Invalid chicken selection data. Expected a numeric ID.", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}

	var selectedChicken Chicken
	foundChicken := false
	for _, ch := range availableChickens { // Ensure availableChickens is loaded
		if ch.ID == chickenID {
			selectedChicken = ch
			foundChicken = true
			break
		}
	}
	if !foundChicken {
		log.Printf("placeBetHandler: Chicken with ID %d not found in availableChickens list.", chickenID)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: fmt.Sprintf("The selected chicken (ID: %d) is not available for betting. (Is availableChickens cache up to date?)", chickenID), NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}
	log.Printf("placeBetHandler: User %d attempting to bet %.2f on chicken ID %d (%s, Odds: %.2f)", currentUserID, betAmount, selectedChicken.ID, selectedChicken.Name, selectedChicken.Odds)

	// Transaction starts here
	tx, err := db.Begin()
	if err != nil {
		log.Printf("placeBetHandler: Failed to begin transaction: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Database error (begin tx). Please try again later.", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}
	committed := false
	defer func() {
		if !committed {
			errRollback := tx.Rollback()
			if errRollback != nil {
				log.Printf("placeBetHandler: Error rolling back transaction: %v (Original failure should be logged above)", errRollback)
			} else {
				log.Println("placeBetHandler: Transaction successfully rolled back due to an earlier error (see logs above).")
			}
		}
	}()

	activeRaceID, err := getActiveRaceID(tx)
	if err != nil {
		log.Printf("placeBetHandler: Error from getActiveRaceID: %v. Rolling back.", err)
		msg := "Failed to determine active race. Please try again."
		if strings.Contains(err.Error(), "no race currently scheduled") {
			msg = "No races are currently open for betting."
		}
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: msg, NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}
	log.Printf("placeBetHandler: Active race for betting determined as ID %d.", activeRaceID)

	var raceStatus string
	err = tx.QueryRow("SELECT status FROM races WHERE id = ?", activeRaceID).Scan(&raceStatus)
	if err != nil {
		log.Printf("placeBetHandler: Error scanning race status for race ID %d: %v. Rolling back.", activeRaceID, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Error confirming race status.", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}
	if raceStatus != RaceStatusScheduled {
		log.Printf("placeBetHandler: Race ID %d status is '%s', not '%s'. Betting closed for this race. Rolling back.", activeRaceID, raceStatus, RaceStatusScheduled)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Betting for this race has closed (Status not 'Scheduled').", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}
	log.Printf("placeBetHandler: Race ID %d status is '%s', OK for betting.", activeRaceID, raceStatus)

	var currentUserBalanceInTx float64
	err = tx.QueryRow("SELECT balance FROM users WHERE id = ?", currentUserID).Scan(&currentUserBalanceInTx)
	if err != nil {
		log.Printf("placeBetHandler: Error scanning user balance for user ID %d: %v. Rolling back.", currentUserID, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Error fetching user balance.", NewBalance: -1}) // Pass -1 as balance couldn't be fetched
		return
	}
	if currentUserBalanceInTx < betAmount {
		log.Printf("placeBetHandler: User %d has insufficient funds (%.2f) for bet amount %.2f. Rolling back.", currentUserID, currentUserBalanceInTx, betAmount)
		_ = betResponseTemplate.Execute(w, BetResponse{
			Success:     false,
			Message:     fmt.Sprintf("Insufficient funds. Your balance is %.2f credits.", currentUserBalanceInTx),
			NewBalance:  currentUserBalanceInTx,
			BetAmount:   betAmount,
			ChickenName: selectedChicken.Name,
		})
		return
	}
	log.Printf("placeBetHandler: User %d balance %.2f is sufficient for bet amount %.2f.", currentUserID, currentUserBalanceInTx, betAmount)

	pendingStatusID, err := getPendingBetStatusID(tx)
	if err != nil {
		log.Printf("placeBetHandler: Error from getPendingBetStatusID: %v. Rolling back.", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "System error: Bet status config.", NewBalance: currentUserBalanceInTx})
		return
	}
	log.Printf("placeBetHandler: Pending bet status ID: %d.", pendingStatusID)

	newBalance := currentUserBalanceInTx - betAmount
	_, err = tx.Exec("UPDATE users SET balance = ? WHERE id = ?", newBalance, currentUserID)
	if err != nil {
		log.Printf("placeBetHandler: Error executing UPDATE users for user ID %d to balance %.2f: %v. Rolling back.", currentUserID, newBalance, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to update balance.", NewBalance: currentUserBalanceInTx}) // Show old balance
		return
	}
	log.Printf("placeBetHandler: Successfully updated user %d balance to %.2f.", currentUserID, newBalance)

	potentialPayout := betAmount * selectedChicken.Odds
	_, err = tx.Exec("INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id, potential_payout) VALUES (?, ?, ?, ?, ?, ?)",
		currentUserID, activeRaceID, chickenID, betAmount, pendingStatusID, potentialPayout)
	if err != nil {
		log.Printf("placeBetHandler: Error executing INSERT INTO bets for user %d, race ID %d, chicken ID %d, amount %.2f: %v. Rolling back.", currentUserID, activeRaceID, chickenID, betAmount, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to record bet.", NewBalance: currentUserBalanceInTx}) // Show old balance as TX will rollback user update too
		return
	}
	log.Printf("placeBetHandler: Successfully inserted bet for user %d, race %d, chicken %d.", currentUserID, activeRaceID, chickenID)

	err = tx.Commit()
	if err != nil {
		log.Printf("placeBetHandler: Error committing transaction: %v. Rolling back (implicitly by defer).", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to finalize bet. Please try again.", NewBalance: currentUserBalanceInTx})
		return
	}
	committed = true // Set committed to true ONLY after successful commit
	log.Printf("placeBetHandler: Bet successfully placed and transaction committed for user %d on chicken %d (Race %d) for amount %.2f. New balance: %.2f", currentUserID, chickenID, activeRaceID, betAmount, newBalance)

	response := BetResponse{
		Success:     true,
		Message:     "Bet placed successfully!",
		NewBalance:  newBalance,
		BetAmount:   betAmount,
		ChickenName: selectedChicken.Name,
	}

	errTmpl := betResponseTemplate.Execute(w, response)
	if errTmpl != nil {
		log.Printf("placeBetHandler: Failed to render success response: %v", errTmpl)
		// Note: The bet is already committed at this point. This is a rendering error.
	}
}
