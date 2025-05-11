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

	if r.Method != http.MethodPost {
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Method not allowed", NewBalance: -1})
		return
	}

	currentUserID := 1 // <<< --- !!! PLACEHOLDER: Replace with actual User ID from session !!! --- >>>
	var userCurrentBalanceForErrorDisplay float64 = -1
	if currentUserID != 0 {
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
	for _, ch := range availableChickens {
		if ch.ID == chickenID {
			selectedChicken = ch
			foundChicken = true
			break
		}
	}
	if !foundChicken {
		log.Printf("placeBetHandler: Chicken with ID %d not found in availableChickens list.", chickenID)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: fmt.Sprintf("The selected chicken (ID: %d) is not available for betting.", chickenID), NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Printf("placeBetHandler: Failed to begin transaction: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Database error. Please try again later.", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}
	committed := false
	defer func() {
		if !committed {
			errRollback := tx.Rollback()
			if errRollback != nil {
				log.Printf("placeBetHandler: Error rolling back transaction: %v", errRollback)
			} else {
				log.Println("placeBetHandler: Transaction rolled back.")
			}
		}
	}()

	activeRaceID, err := getActiveRaceID(tx)
	if err != nil {
		log.Printf("placeBetHandler: Could not determine active race for betting: %v", err)
		msg := "Failed to determine active race. Please try again."
		if strings.Contains(err.Error(), "no race currently scheduled") {
			msg = "No races are currently open for betting."
		}
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: msg, NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}

	var raceStatus string
	err = tx.QueryRow("SELECT status FROM races WHERE id = ?", activeRaceID).Scan(&raceStatus)
	if err != nil {
		// ... (error handling as before)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Error confirming race status.", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}
	if raceStatus != RaceStatusScheduled {
		// ... (error handling as before)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Betting for this race has closed.", NewBalance: userCurrentBalanceForErrorDisplay})
		return
	}

	var currentUserBalanceInTx float64
	err = tx.QueryRow("SELECT balance FROM users WHERE id = ?", currentUserID).Scan(&currentUserBalanceInTx)
	if err != nil {
		// ... (error handling as before)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Error fetching user balance."})
		return
	}

	if currentUserBalanceInTx < betAmount {
		_ = betResponseTemplate.Execute(w, BetResponse{
			Success:     false,
			Message:     fmt.Sprintf("Insufficient funds. Your balance is %.2f credits.", currentUserBalanceInTx),
			NewBalance:  currentUserBalanceInTx,
			BetAmount:   betAmount,
			ChickenName: selectedChicken.Name,
		})
		return
	}

	pendingStatusID, err := getPendingBetStatusID(tx)
	if err != nil {
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "System error: Bet status config.", NewBalance: currentUserBalanceInTx})
		return
	}

	newBalance := currentUserBalanceInTx - betAmount
	_, err = tx.Exec("UPDATE users SET balance = ? WHERE id = ?", newBalance, currentUserID)
	if err != nil {
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to update balance.", NewBalance: currentUserBalanceInTx})
		return
	}

	potentialPayout := betAmount * selectedChicken.Odds
	_, err = tx.Exec("INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id, potential_payout) VALUES (?, ?, ?, ?, ?, ?)",
		currentUserID, activeRaceID, chickenID, betAmount, pendingStatusID, potentialPayout)
	if err != nil {
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to record bet.", NewBalance: currentUserBalanceInTx})
		return
	}

	err = tx.Commit()
	if err != nil {
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to finalize bet.", NewBalance: currentUserBalanceInTx})
		return
	}
	committed = true
	log.Printf("placeBetHandler: Bet successfully placed for user %d on chicken %d (Race %d) for amount %.2f. New balance: %.2f", currentUserID, chickenID, activeRaceID, betAmount, newBalance)

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
	}
}