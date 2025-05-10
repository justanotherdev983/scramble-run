package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"math/rand" // Added
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync" // Added
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "github.com/mattn/go-sqlite3"
)



const (
	local_port string = "6969"
	raceInterval        = 1 * time.Minute  // How often to try to schedule a new race if one isn't already. Short for testing.
	raceDuration        = 20 * time.Second // How long a race "runs" before a winner is decided. Short for testing.
	RaceStatusScheduled = "Scheduled"
	RaceStatusRunning   = "Running"
	RaceStatusFinished  = "Finished"
)

var (
	db                  *sql.DB
	baseTemplate        *template.Template
	homeTemplate        *template.Template
	loginTemplate       *template.Template
	signupTemplate      *template.Template
	betResponseTemplate *template.Template // Added for placeBetHandler response
	raceInfoTemplate    *template.Template

	// Global list of chickens, consistent with what's used elsewhere
	availableChickens = []Chicken{
		{ID: 1, Name: "Henrietta", Color: "red", Odds: 2.5, Lane: 10, Progress: 0},
		{ID: 2, Name: "Cluck Norris", Color: "blue", Odds: 3.0, Lane: 50, Progress: 0},
		{ID: 3, Name: "Foghorn", Color: "green", Odds: 4.0, Lane: 90, Progress: 0},
	}

	// Race Management State
	raceMutex             sync.Mutex
	currentRaceDetails    *RaceInfo     // Details of the race currently running or just finished
	nextRaceStartTime     time.Time     // Calculated start time of the next scheduled race
	raceTicker            *time.Ticker  // For the main race loop checking
	raceEndTimer          *time.Timer   // For timing the duration of a running race
	isRaceSystemActive    bool      = false // To control the race loop, useful for shutdown
)

type RaceInfo struct {
	Id              int
	Name            string
	Winner          string        // Name of the winning chicken
	WinnerChickenID sql.NullInt64 // ID of the winning chicken from DB (can be NULL)
	ChickenNames    []string      // Names of chickens participating (can be dynamic later)
	Date            time.Time     // Scheduled Start Time
	Status          string        // 'Scheduled', 'Running', 'Finished'
}

type Chicken struct {
	ID       int
	Name     string
	Color    string
	Odds     float64
	Lane     int
	Progress float64
}

type ActiveRace struct {
	Chickens []Chicken
}

type User struct {
	ID    int
	Name  string
	Email string
	Age   int
	// Balance float64 // Consider adding Balance here if you fetch full user data often
}

type PageData struct {
	Title       string
	UserData    User
	UserBalance float64    // ADDED: To display user's current balance
	Races       []RaceInfo
	Chickens    []Chicken
	ActiveRace  ActiveRace

	// For initial rendering by homeHandler, HTMX will take over for subsequent updates
	InitialNextRaceTime     string    // Formatted string: "MM:SS" or status message
	InitialStatusMessage    string    // e.g., "Next race in:", "Race in Progress:"
	InitialRaceName         string    // Name of the current/next race for initial display
	IsBettingInitiallyOpen  bool      // Betting status for initial display
	CurrentRaceDisplay      *RaceInfo // Still useful for other race details if needed

	PotentialWinnings float64
	Message           string
	Success           bool
}

type WinningsCalc struct {
	Amount    float64
	ChickenID int
}

type BetResponse struct {
	Success     bool
	Message     string
	NewBalance  float64
	BetAmount   float64
	ChickenName string
}

type rowQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func init() {
	var err error
	db = init_database() // Call before template parsing that might need DB
	if db == nil {
		log.Fatal("Database initialization failed")
		return
	}
    // Seed random number generator (used for race names, winner selection)
    rand.Seed(time.Now().UnixNano())


	baseTemplate, err = template.ParseFiles("src/web/templates/base.gohtml")
	if err != nil {
		log.Fatalf("Error parsing base template: %v", err)
	}

	homeTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/home.gohtml")
	if err != nil {
		log.Fatalf("Error parsing home template: %v", err)
	}

	loginTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/login.gohtml")
	if err != nil {
		log.Fatalf("Error parsing login template: %v", err)
	}

	signupTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/signup.gohtml")
	if err != nil {
		log.Fatalf("Error parsing signup template: %v", err)
	}

	betResponseTemplate = template.Must(template.New("betResponse").Parse(`
		{{/* This is the content for #bet-response-area */}}
		<div class="bet-response" id="bet-response-content">
			{{if .Success}}
				<div class="alert alert-success">
					<p>{{.Message}}</p>
					<p>Bet placed: {{printf "%.2f" .BetAmount}} credits on {{.ChickenName}}</p>
					<p>New balance: {{printf "%.2f" .NewBalance}} credits</p>
				</div>
			{{else}}
				<div class="alert alert-danger">
					<p>{{.Message}}</p>
					{{if ge .NewBalance 0.0}} <!-- Show balance even on failure if it's sensible -->
					<p>Your balance: {{printf "%.2f" .NewBalance}} credits</p>
					{{end}}
				</div>
			{{end}}
		</div>

		{{/* Out-of-Band Swap to update the user balance display elsewhere on the page */}}
		<span id="user-balance-display" hx-swap-oob="true">{{printf "%.2f" .NewBalance}}</span>
	`))

	// Template for HTMX race info updates
	raceInfoTemplate = template.Must(template.New("raceInfoSnippet").Parse(`
        <div id="race-timer-display"> <!-- This ID should match the one in home.gohtml -->
            <p class="race-status-message">{{.StatusMsg}} <span class="race-name-display">{{.RaceName}}</span></p>
            {{if .CountdownStr}}
                <p class="countdown-timer">{{.CountdownStr}}</p>
            {{end}}
            {{if .IsBettingOpen}}
                <p class="betting-status betting-open">Betting is Open!</p>
            {{else if .IsRaceRunning}}
                <p class="betting-status betting-closed">Betting Closed (Race Running)</p>
            {{else}}
                 <p class="betting-status betting-closed">Betting is Closed</p>
            {{end}}
        </div>
    `))

	raceInfoTemplate = template.Must(template.New("raceInfoSnippet").Parse(`
		{{/* This is the entire new innerHTML for div#race-timer-dynamic-area */}}
		<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="race-timer-icon">
			<circle cx="12" cy="12" r="10"></circle>
			<polyline points="12 6 12 12 16 14"></polyline>
		</svg>
		<span class="race-timer-prefix">
			{{/* Determine prefix based on StatusMsg or CountdownStr presence */}}
			{{ if .IsRaceRunning }}
				Race in Progress:
			{{ else if .CountdownStr }}
				Next race in:
			{{ else if .StatusMsg }}
				{{/* StatusMsg itself might be descriptive, e.g., "Last race finished:" */}}
				{{/* Let it be blank if StatusMsg is the main info */}}
			{{ else }}
				Status:
			{{ end }}
		</span>
		<span class="race-timer-countdown">
			{{if .CountdownStr}}
				{{.CountdownStr}}
			{{/* If race is running, StatusMsg handles "Race in Progress:", RaceName has the actual name.
			CountdownStr for a running race is usually not the primary display here. */}}
			{{else if .StatusMsg}}
				{{.StatusMsg}} {{/* Catches "Next one soon...", "Checking schedule...", "Race in Progress" (if no countdown) */}}
			{{else}}
				--:--
			{{end}}
		</span>
		{{if .RaceName}}
			<span class="race-timer-racename">({{ .RaceName }})</span>
		{{end}}
		<br> {{/* Or use CSS for layout */}}
		<span class="race-timer-bettingstatus">
			{{if .IsBettingOpen}}
				Betting is Open!
			{{else if .IsRaceRunning}}
				Betting Closed (Race Running)
			{{else}}
				Betting is Closed
			{{end}}
		</span>
	`))

	log.Println("Templates loaded successfully")
}

func init_database() *sql.DB {
	db, err := sql.Open("sqlite3", "src/internal/database/scramble.db")
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return nil
	}

	// More comprehensive check for initialization need
	shouldInitialize := false
	var tableCount int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('users', 'races', 'chickens', 'bets', 'bet_statuses');").Scan(&tableCount)
	if err != nil {
		log.Printf("Failed to check for core tables: %v. Assuming initialization is needed.", err)
		shouldInitialize = true
	} else if tableCount < 5 { // Expecting 5 core tables now
		log.Printf("Found %d core tables, expected 5. Database might be incomplete. Attempting initialization.", tableCount)
		shouldInitialize = true
	} else {
		// Check for 'balance' column in 'users'
		var balanceColExists int
		err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='balance'").Scan(&balanceColExists)
		if err != nil || balanceColExists == 0 {
			log.Println("'balance' column not found in 'users' or error checking. Database might need re-initialization.")
			shouldInitialize = true
		}
        // Check for 'status' column in 'races'
        var statusColExistsRaces int
        err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('races') WHERE name='status'").Scan(&statusColExistsRaces)
        if err != nil || statusColExistsRaces == 0 {
            log.Println("'status' column not found in 'races' or error checking. Database might need re-initialization.")
            shouldInitialize = true
        }
	}

	if shouldInitialize {
		log.Println("Attempting to initialize database from SQL file...")
		sql_file, err_read := os.ReadFile("src/internal/database/init_database.sql")
		if err_read != nil {
			log.Printf("Failed to read SQL initialization file: %v", err_read)
			db.Close()
			return nil
		}

		_, err_exec := db.Exec(string(sql_file)) // SQLite driver typically handles multiple statements
		if err_exec != nil {
			log.Printf("Failed to initialize the database: %v", err_exec)
			db.Close()
			return nil
		}
		fmt.Println("Database initialized/verified successfully from SQL file.")
	} else {
		fmt.Println("Database structure appears up-to-date.")
	}

	return db
}

func generateRaceName() string {
	adjectives := []string{"Speedy", "Thunder", "Golden", "Lightning", "Cosmic", "農場 (Farm)", "Feathered", "Clucky"}
	nouns := []string{"Derby", "Sprint", "Classic", "Gallop", "Frenzy", "Run", "Cup", "Challenge"}
	// Add a unique touch with a small random number or part of a timestamp
	return fmt.Sprintf("%s %s #%d", adjectives[rand.Intn(len(adjectives))], nouns[rand.Intn(len(nouns))], rand.Intn(1000))
}

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
		// An active (Scheduled or Running) race already exists. Fetch its start time if Scheduled.
		var status string
		var dateStr string
		err = db.QueryRow("SELECT status, date FROM races WHERE status = ? OR status = ? ORDER BY date ASC LIMIT 1", RaceStatusScheduled, RaceStatusRunning).Scan(&status, &dateStr)
		if err == nil {
			parsedTime, _ := parseRaceDate(dateStr) // Use existing helper
			if status == RaceStatusScheduled {
				nextRaceStartTime = parsedTime
				currentRaceDetails = nil // No race is "currently running" if we just found a scheduled one
				log.Printf("scheduleNewRace: A race is already scheduled for %v. No new race created.", nextRaceStartTime)
			} else { // RaceStatusRunning
				// If a race is running, nextRaceStartTime shouldn't be set by this function.
				// The raceLoop will wait for it to finish.
                // Load current race details if not already loaded (e.g., on startup)
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
		return false, nil // No new race was scheduled by this call
	}

	// No 'Scheduled' or 'Running' race, so create a new one
	scheduledTime := time.Now().Add(raceInterval)
	raceName := generateRaceName()

	result, err := db.Exec("INSERT INTO races (name, date, status) VALUES (?, ?, ?)", raceName, scheduledTime, RaceStatusScheduled)
	if err != nil {
		log.Printf("scheduleNewRace: Error inserting new race: %v", err)
		return false, err
	}
	newRaceID64, _ := result.LastInsertId()
	nextRaceStartTime = scheduledTime
	currentRaceDetails = nil // Clear any finished race details

	log.Printf("Scheduled new race: ID %d, Name: '%s', StartTime: %v", newRaceID64, raceName, scheduledTime)
	return true, nil // New race was scheduled
}

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
		// Check current status to see why
		var currentStatus string
		db.QueryRow("SELECT status FROM races WHERE id = ?", raceID).Scan(&currentStatus)
		log.Printf("startRace: Current status of race %d is '%s'", raceID, currentStatus)
		return fmt.Errorf("race %d not in 'Scheduled' state", raceID)
	}


	raceInfo, err := getRaceDetails(db, raceID) // Fetch details for the now running race
	if err != nil {
		log.Printf("startRace: Could not fetch details for running race %d: %v. Using minimal info.", raceID, err)
		currentRaceDetails = &RaceInfo{Id: raceID, Status: RaceStatusRunning, Name: "Race " + strconv.Itoa(raceID)}
	} else {
		currentRaceDetails = raceInfo // This is now the globally visible "current race"
	}
    // Ensure status is correctly set, getRaceDetails might fetch old status if DB tx not complete
    if currentRaceDetails != nil {
        currentRaceDetails.Status = RaceStatusRunning
    }


	log.Printf("Race ID: %d (%s) started. Will finish in %v.", raceID, currentRaceDetails.Name, raceDuration)
	nextRaceStartTime = time.Time{} // Clear next scheduled time as this one is now running
	raceMutex.Unlock() // Unlock before setting timer to avoid deadlock if timer func needs mutex

	// Set a timer to finish the race
	if raceEndTimer != nil {
		raceEndTimer.Stop()
	}
	raceEndTimer = time.AfterFunc(raceDuration, func() {
		log.Printf("Race end timer fired for race ID: %d", raceID)
		err := finishRace(db, raceID)
		if err != nil {
			log.Printf("Error auto-finishing race %d: %v", raceID, err)
		}
		// After finishing, the main raceLoop will schedule the next one.
	})
	return nil
}



func finishRace(db *sql.DB, raceID int) error {
	raceMutex.Lock()
	// defer raceMutex.Unlock() // Defer until after settleBets potentially needs the lock or a new one.

	log.Printf("Attempting to finish race ID: %d", raceID)

	var currentStatus string
	err := db.QueryRow("SELECT status FROM races WHERE id = ?", raceID).Scan(&currentStatus)
	if err != nil {
		raceMutex.Unlock() // Unlock before early return
		if err == sql.ErrNoRows {
			log.Printf("finishRace: Race ID %d not found.", raceID)
			return fmt.Errorf("race %d not found", raceID)
		}
		log.Printf("finishRace: Error querying status for race %d: %v", raceID, err)
		return err
	}

	if currentStatus != RaceStatusRunning {
		raceMutex.Unlock() // Unlock before early return
		log.Printf("finishRace: Race ID %d is not 'Running' (status: %s). Cannot finish.", raceID, currentStatus)
		if currentStatus == RaceStatusFinished {
			return nil
		}
		return fmt.Errorf("race %d is not 'Running', status is %s", raceID, currentStatus)
	}

	if len(availableChickens) == 0 {
		log.Printf("finishRace: No available chickens to determine a winner for race %d.", raceID)
		_, errDb := db.Exec("UPDATE races SET status = ?, winner_chicken_id = NULL WHERE id = ?", RaceStatusFinished, raceID)
		if currentRaceDetails != nil && currentRaceDetails.Id == raceID {
			currentRaceDetails.Status = RaceStatusFinished
			currentRaceDetails.Winner = "N/A (No chickens)"
			currentRaceDetails.WinnerChickenID = sql.NullInt64{Valid: false}
		}
		raceMutex.Unlock() // Unlock before early return
		return errDb
	}

	winnerChicken := availableChickens[rand.Intn(len(availableChickens))]
	log.Printf("Race ID: %d finished. Winner: %s (ID: %d)", raceID, winnerChicken.Name, winnerChicken.ID)

	// Use a transaction for updating race and settling bets
	tx, errTx := db.Begin()
	if errTx != nil {
		raceMutex.Unlock() // Unlock before early return
		log.Printf("finishRace: Failed to begin transaction for race %d: %v", raceID, errTx)
		return errTx
	}
	committed := false
	defer func() {
		if !committed {
			errRollback := tx.Rollback()
			if errRollback != nil {
				log.Printf("finishRace: Error rolling back transaction for race %d: %v", raceID, errRollback)
			}
		}
	}()

	_, err = tx.Exec("UPDATE races SET status = ?, winner_chicken_id = ? WHERE id = ?", RaceStatusFinished, winnerChicken.ID, raceID)
	if err != nil {
		raceMutex.Unlock() // Unlock before early return (tx will be rolled back by defer)
		log.Printf("finishRace: Error updating race %d to Finished in DB: %v", raceID, err)
		return err
	}

	// Settle bets for this race
	errSettle := settleBetsForRace(tx, raceID, winnerChicken.ID)
	if errSettle != nil {
		raceMutex.Unlock() // Unlock before early return (tx will be rolled back)
		log.Printf("finishRace: Error settling bets for race %d: %v", raceID, errSettle)
		return errSettle // This will cause a rollback
	}

	errCommit := tx.Commit()
	if errCommit != nil {
		raceMutex.Unlock() // Unlock before early return
		log.Printf("finishRace: Error committing transaction for race %d: %v", raceID, errCommit)
		return errCommit
	}
	committed = true

	// Update global currentRaceDetails *after successful commit*
	// It's better to fetch the fully updated details if necessary or construct carefully
	if currentRaceDetails != nil && currentRaceDetails.Id == raceID {
		currentRaceDetails.Status = RaceStatusFinished
		currentRaceDetails.Winner = winnerChicken.Name
		currentRaceDetails.WinnerChickenID = sql.NullInt64{Int64: int64(winnerChicken.ID), Valid: true}
	} else {
		// If currentRaceDetails was for a different race, or nil, update it to this finished one
		// This ensures the UI shows the most recently finished race details.
		updatedRaceInfo, _ := getRaceDetails(db, raceID) // Use main db connection, not tx
		if updatedRaceInfo != nil {
			currentRaceDetails = updatedRaceInfo
		}
	}
	raceMutex.Unlock() // Unlock after all critical operations

	log.Printf("Race %d successfully marked as Finished. Winner: %s. Bets settled. The raceLoop will schedule.", raceID, winnerChicken.Name)
	return nil
}

// settleBetsForRace processes all 'Pending' bets for a finished race.
// It updates user balances and bet statuses within the provided transaction.
func settleBetsForRace(tx *sql.Tx, raceID int, winningChickenID int) error {
	log.Printf("Settling bets for Race ID: %d, Winning Chicken ID: %d", raceID, winningChickenID)

	// Get status IDs for 'Won' and 'Lost'
	var wonStatusID, lostStatusID int
	err := tx.QueryRow("SELECT id FROM bet_statuses WHERE status_name = 'Won'").Scan(&wonStatusID)
	if err != nil {
		return fmt.Errorf("could not find 'Won' bet status ID: %w", err)
	}
	err = tx.QueryRow("SELECT id FROM bet_statuses WHERE status_name = 'Lost'").Scan(&lostStatusID)
	if err != nil {
		return fmt.Errorf("could not find 'Lost' bet status ID: %w", err)
	}
	pendingStatusID, err := getPendingBetStatusID(tx) // Get pending status ID within tx
	if err != nil {
		return fmt.Errorf("could not find 'Pending' bet status ID for settling: %w", err)
	}


	// Select all pending bets for this race
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

	for rows.Next() {
		var betID, userID, betChickenID int
		var betAmount, chickenOdds float64
		if err := rows.Scan(&betID, &userID, &betChickenID, &betAmount, &chickenOdds); err != nil {
			log.Printf("settleBetsForRace: Error scanning bet row for race %d: %v", raceID, err)
			continue // Or return error to rollback all settlements
		}

		var payout float64 = 0
		newStatusID := lostStatusID

		if betChickenID == winningChickenID {
			payout = betAmount * chickenOdds
			newStatusID = wonStatusID
			log.Printf("Bet ID %d (User %d) on chicken %d WON. Bet: %.2f, Odds: %.2f, Payout: %.2f",
				betID, userID, betChickenID, betAmount, chickenOdds, payout)

			// Update user's balance
			_, errUpdateBalance := tx.Exec("UPDATE users SET balance = balance + ? WHERE id = ?", payout, userID)
			if errUpdateBalance != nil {
				log.Printf("settleBetsForRace: Failed to update balance for user %d after winning bet %d: %v", userID, betID, errUpdateBalance)
				return fmt.Errorf("failed to update balance for user %d on win: %w", userID, errUpdateBalance)
			}
		} else {
			log.Printf("Bet ID %d (User %d) on chicken %d LOST. Winning chicken was %d.",
				betID, userID, betChickenID, winningChickenID)
			// No change to balance for losing, bet amount was already deducted.
		}

		// Update bet status and actual payout
		_, errUpdateBet := tx.Exec("UPDATE bets SET bet_status_id = ?, actual_payout = ? WHERE id = ?", newStatusID, payout, betID)
		if errUpdateBet != nil {
			log.Printf("settleBetsForRace: Failed to update status for bet %d: %v", betID, errUpdateBet)
			return fmt.Errorf("failed to update status for bet %d: %w", betID, errUpdateBet)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating bet rows for race %d: %w", raceID, err)
	}

	log.Printf("All pending bets for race %d processed.", raceID)
	return nil
}

func raceLoop(db *sql.DB) {
	isRaceSystemActive = true
	log.Println("Race Manager: Starting race loop...")

	// Attempt initial scheduling immediately.
	// scheduleNewRace will set nextRaceStartTime if it schedules one,
	// or if it finds an existing scheduled race.
	_, err := scheduleNewRace(db)
	if err != nil {
		log.Printf("Race Manager: Initial race scheduling/check failed: %v. Will retry via ticker.", err)
	}

	raceTicker = time.NewTicker(5 * time.Second) // Check status every 5 seconds
	defer raceTicker.Stop()
	if raceEndTimer != nil { // Should be nil at start, but defensive
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

			// log.Printf("Race Manager Tick: Next Scheduled: %v, Current Race: %v", rnNextRaceStartTime, rnCurrentRace)


			if rnCurrentRace != nil && rnCurrentRace.Status == RaceStatusRunning {
				// A race is running. The raceEndTimer will handle its completion.
				// log.Printf("Race Manager: Race %d (%s) is %s. Waiting for it to finish.", rnCurrentRace.Id, rnCurrentRace.Name, rnCurrentRace.Status)
				continue
			}

			// If a race is scheduled and it's time to start it
			if !rnNextRaceStartTime.IsZero() && time.Now().After(rnNextRaceStartTime) {
                // This means a scheduled race's time has come. Find its ID.
                // It must be the earliest 'Scheduled' race whose time has passed.
				var raceToStartID int
				var raceNameToStart string
				err := db.QueryRow("SELECT id, name FROM races WHERE status = ? AND date <= ? ORDER BY date ASC LIMIT 1",
					RaceStatusScheduled, time.Now()).Scan(&raceToStartID, &raceNameToStart)

				if err == nil {
					log.Printf("Race Manager: Time to start race ID %d ('%s'). Starting now.", raceToStartID, raceNameToStart)
					errStart := startRace(db, raceToStartID)
					if errStart != nil {
						log.Printf("Race Manager: Failed to start race %d: %v. It might be stuck or was already processed.", raceToStartID, errStart)
						// If starting failed, it might be stuck. We might need to mark it as 'Errored'
						// or the next scheduleNewRace call might try to schedule another if this one isn't 'Running'.
                        // For now, clear nextRaceStartTime to force re-evaluation by scheduleNewRace.
                        raceMutex.Lock()
                        nextRaceStartTime = time.Time{}
                        raceMutex.Unlock()
					}
					// After calling startRace, the loop will continue, and the next tick
                    // will see if the race is Running or if a new one needs scheduling.
					continue
				} else if err != sql.ErrNoRows {
					log.Printf("Race Manager: Error finding scheduled race to start whose time has passed: %v", err)
				}
				// If ErrNoRows, means the race might have been started by another instance/process,
				// or an issue with timing. scheduleNewRace below will re-evaluate.
			}

			// If no race is running AND (no race is scheduled OR scheduled time is far off/stale), try to schedule a new one.
            // This covers: startup, after a race finishes, or if a scheduled race failed to start.
			if (rnCurrentRace == nil || rnCurrentRace.Status == RaceStatusFinished) || rnNextRaceStartTime.IsZero() {
				// log.Printf("Race Manager: No active race or next race time is zero/past. Checking/Scheduling new race.")
				scheduled, scheduleErr := scheduleNewRace(db)
				if scheduleErr != nil {
					log.Printf("Race Manager: Error during periodic scheduling: %v", scheduleErr)
				} else if scheduled {
					// log.Printf("Race Manager: A new race was scheduled by the ticker.")
				} else {
					// log.Printf("Race Manager: scheduleNewRace decided not to schedule a new race this tick (one likely exists or is running).")
				}
			}
		}
	}
}

func parseRaceDate(dateStr string) (time.Time, error) {
    // Common SQLite datetime formats. RFC3339 is often used by Go's time.Time.String()
    // but SQLite might store it differently if not explicitly formatted.
    layouts := []string{
        time.RFC3339,                          // "2006-01-02T15:04:05Z07:00"
		"2006-01-02 15:04:05.999999999-07:00", // SQLite default with timezone
		"2006-01-02 15:04:05-07:00",           // RFC3339 without sub-seconds
		"2006-01-02 15:04:05",                 // Common SQLite format (often local time)
		"2006-01-02T15:04:05Z",                // ISO8601 UTC
		"2006-01-02",                          // Date only
    }
    var parsedTime time.Time
    var err error
    for _, layout := range layouts {
        parsedTime, err = time.ParseInLocation(layout, dateStr, time.Local) // Assume Local if no zone info
        if err == nil {
            return parsedTime, nil
        }
    }
    // If all fail, log and return the last error
    // log.Printf("parseRaceDate: Failed to parse date string '%s' using multiple formats. Last error: %v", dateStr, err)
    return time.Time{}, fmt.Errorf("failed to parse date string '%s': %w", dateStr, err)
}

func getRaceDetails(querier rowQuerier, raceID int) (*RaceInfo, error) {
	var race RaceInfo
	var dateStr string
	var winnerID sql.NullInt64   // For winner_chicken_id from DB
	var winnerName sql.NullString // For winner's name from JOIN

	query := `
        SELECT r.id, r.name, r.date, r.status, r.winner_chicken_id, c.name AS winner_name
        FROM races r
        LEFT JOIN chickens c ON r.winner_chicken_id = c.id
        WHERE r.id = ?
    `
	err := querier.QueryRow(query, raceID).Scan(&race.Id, &race.Name, &dateStr, &race.Status, &winnerID, &winnerName)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("race with ID %d not found", raceID)
		}
		return nil, fmt.Errorf("error fetching race details for ID %d: %w", raceID, err)
	}

	parsedTime, errParse := parseRaceDate(dateStr) // Use helper
	if errParse != nil {
		log.Printf("getRaceDetails: Failed to parse date '%s' for race ID %d: %v", dateStr, race.Id, errParse)
		race.Date = time.Time{} // Zero time on parse failure
	} else {
		race.Date = parsedTime
	}

	race.WinnerChickenID = winnerID
	if winnerName.Valid {
		race.Winner = winnerName.String
	} else if winnerID.Valid { // Winner ID exists but name couldn't be joined (e.g. chicken deleted)
		race.Winner = fmt.Sprintf("Chicken ID %d (name unknown)", winnerID.Int64)
	} else {
		race.Winner = "N/A" // No winner yet or not applicable
	}


	// Populate ChickenNames (assuming all availableChickens participate in every race for now)
	race.ChickenNames = make([]string, len(availableChickens))
	for i, ch := range availableChickens {
		race.ChickenNames[i] = ch.Name
	}

	return &race, nil
}



func getActiveRaceID(db rowQuerier) (int, error) {
	var raceID int
	// Select the race with the latest date that does not have a winner.
	// Order by date DESC ensures we prefer later scheduled races.
	// Order by id DESC is a tie-breaker for races on the same date.
	err := db.QueryRow("SELECT id FROM races WHERE winner IS NULL OR winner = '' ORDER BY date DESC, id DESC LIMIT 1").Scan(&raceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("no active race available for betting at the moment")
		}
		return 0, fmt.Errorf("error fetching active race ID: %w", err)
	}
	return raceID, nil
}

func getPendingBetStatusID(db rowQuerier) (int, error) {
	var statusID int
	err := db.QueryRow("SELECT id FROM bet_statuses WHERE status_name = 'Pending'").Scan(&statusID)
	if err != nil {
		if err == sql.ErrNoRows {
			// This is a critical setup error if 'Pending' status doesn't exist.
			return 0, fmt.Errorf("bet status 'Pending' not found in database; please ensure bet_statuses table is initialized correctly")
		}
		return 0, fmt.Errorf("error fetching 'Pending' bet status ID: %w", err)
	}
	return statusID, nil
}

func get_races(db *sql.DB) []RaceInfo {
	rows, err := db.Query(`
        SELECT r.id, r.name, r.date, r.status, r.winner_chicken_id, c.name AS winner_name
        FROM races r
        LEFT JOIN chickens c ON r.winner_chicken_id = c.id
        ORDER BY r.date DESC
    `) // Fetch winner name directly
	if err != nil {
		log.Printf("get_races: Failed to query races: %v", err)
		return nil
	}
	defer rows.Close()

	var races []RaceInfo
	for rows.Next() {
		var race RaceInfo
		var dateStr string
		var winnerID sql.NullInt64   // For r.winner_chicken_id
		var winnerName sql.NullString // For c.name AS winner_name

		err = rows.Scan(&race.Id, &race.Name, &dateStr, &race.Status, &winnerID, &winnerName)
		if err != nil {
			log.Printf("get_races: Failed to scan row: %v", err)
			continue
		}

		parsedTime, errParse := parseRaceDate(dateStr)
		if errParse != nil {
			log.Printf("get_races: Failed to parse date string '%s' for race ID %d: %v", dateStr, race.Id, errParse)
			race.Date = time.Time{} // Set to zero time if parsing fails
		} else {
			race.Date = parsedTime
		}

		race.WinnerChickenID = winnerID // Store the ID as well
		if winnerName.Valid {
			race.Winner = winnerName.String
		} else if winnerID.Valid { // ID exists but name join failed
			race.Winner = fmt.Sprintf("Chicken ID %d", winnerID.Int64)
		} else if race.Status == RaceStatusFinished {
			race.Winner = "N/A (Winner not recorded)"
		} else {
			race.Winner = "" // Or "Pending", "Not run" etc.
		}
		// ChickenNames can be populated if needed per race, for now, it's empty here.
		// For detailed view of a single race, you might populate it.
		races = append(races, race)
	}
	if err := rows.Err(); err != nil {
		log.Printf("get_races: Error iterating through rows: %v", err)
	}
	return races
}

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
			raceEndTimer.Stop() // Stop the natural timer
		}
		// Call finishRace directly.
		err := finishRace(db, currentRace.Id) // finishRace is responsible for DB and global state
		if err != nil {
			log.Printf("ADMIN: Error force-finishing race %d: %v", currentRace.Id, err)
			forcedAction = fmt.Sprintf("Error force-finishing race %d: %v", currentRace.Id, err)
		} else {
			log.Printf("ADMIN: Race %d force-finished.", currentRace.Id)
			forcedAction = fmt.Sprintf("Race %d force-finished.", currentRace.Id)
		}
	} else {
		// If no race is running, see if one is scheduled that we can force-start
		var raceToStartID int
		var raceToStartName string
		var raceToStartDate time.Time
		var dateStrToStart string

		// Find the next scheduled race, regardless of its time
		err := db.QueryRow("SELECT id, name, date FROM races WHERE status = ? ORDER BY date ASC LIMIT 1",
			RaceStatusScheduled).Scan(&raceToStartID, &raceToStartName, &dateStrToStart)

		if err == nil {
			raceToStartDate, _ = parseRaceDate(dateStrToStart)
			log.Printf("ADMIN: Forcing start for next scheduled race ID %d: %s (Original time: %v)", raceToStartID, raceToStartName, raceToStartDate)
			// To force start, we can update its date to now, then let the regular loop pick it up, or call startRace.
			// Forcing it via startRace is more direct.
			// _, errUpdate := db.Exec("UPDATE races SET date = ? WHERE id = ?", time.Now(), raceToStartID) // Option 1
			// if errUpdate != nil { log.Printf("ADMIN: Error updating race date for force start: %v", errUpdate)}
			errStart := startRace(db, raceToStartID) // Option 2: Direct call
			if errStart != nil {
				log.Printf("ADMIN: Error force-starting race %d: %v", raceToStartID, errStart)
				forcedAction = fmt.Sprintf("Error force-starting race %d: %v", raceToStartID, errStart)
			} else {
				forcedAction = fmt.Sprintf("Race %d (%s) force-started.", raceToStartID, raceToStartName)
			}
		} else if err == sql.ErrNoRows {
			log.Println("ADMIN: No race running and no race scheduled to force-start.")
            forcedAction = "No race running or scheduled to force."
            // Try to schedule one now
            scheduled, sErr := scheduleNewRace(db)
            if sErr != nil { forcedAction += " Error scheduling new: " + sErr.Error() }
            if scheduled { forcedAction += " New race scheduled."}

		} else {
			log.Printf("ADMIN: Error finding race to force-start: %v", err)
            forcedAction = "Error finding race to force: " + err.Error()
		}
	}

	// Kick the main raceTicker to re-evaluate state immediately after forced action
	if raceTicker != nil {
		log.Println("ADMIN: Resetting raceTicker to re-evaluate scheduling post-trigger.")
		raceTicker.Reset(100 * time.Millisecond) // Very short to make it run almost now
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Race cycle triggered. Action: %s. Check server logs.\n", forcedAction)
	log.Printf("ADMIN: Manual race cycle trigger processed. Action: %s", forcedAction)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: Get userID from session to fetch actual user data and balance
	currentUserID := 1 // <<<< --- !!! PLACEHOLDER: Replace with actual User ID from session !!! --- >>>
	var currentUser User
	var userBalance float64 // Fetch the actual balance

	if currentUserID != 0 {
		// Fetch user details AND balance
		errDb := db.QueryRow("SELECT id, name, email, balance FROM users WHERE id = ?", currentUserID).Scan(&currentUserID, &currentUser.Name, &currentUser.Email, &userBalance)
		if errDb != nil {
			if errDb == sql.ErrNoRows {
				log.Printf("homeHandler: User ID %d not found. Displaying as Guest.", currentUserID)
				currentUser.Name = "Guest" // Or redirect to login
				userBalance = 0
			} else {
				log.Printf("homeHandler: Error fetching user data for ID %d: %v", currentUserID, errDb)
				currentUser.Name = "Error" // Or handle error more gracefully
				userBalance = 0
			}
		}
	} else {
		currentUser.Name = "Guest"
		userBalance = 0
	}


	data := PageData{
		Title:             "Scramble Run",
		UserData:          currentUser, // Use fetched or Guest user
		UserBalance:       userBalance, // Pass the fetched balance
		Races:             get_races(db),
		Chickens:          availableChickens,
		ActiveRace:        ActiveRace{Chickens: availableChickens},
		PotentialWinnings: 0.0,
	}

	raceMutex.Lock()
	pageNextRaceStartTime := nextRaceStartTime
	pageCurrentRaceDetails := currentRaceDetails
	raceMutex.Unlock()

	var calculatedTimeStr string
	var calculatedStatusMsg string // More descriptive status
	var calculatedRaceName string  // Name for current/next race
	isBettingInitiallyOpen := false

	if pageCurrentRaceDetails != nil && pageCurrentRaceDetails.Status == RaceStatusRunning {
		calculatedStatusMsg = "Race in Progress:"
		calculatedRaceName = pageCurrentRaceDetails.Name
		calculatedTimeStr = "Running!" // Or time remaining in race if you implement that
		isBettingInitiallyOpen = false
	} else if !pageNextRaceStartTime.IsZero() && pageNextRaceStartTime.After(time.Now()) {
		durationUntilNext := time.Until(pageNextRaceStartTime)
		if durationUntilNext > 0 {
			minutes := int(durationUntilNext.Minutes())
			seconds := int(durationUntilNext.Seconds()) % 60
			calculatedTimeStr = fmt.Sprintf("%02d:%02d", minutes, seconds)
			calculatedStatusMsg = "Next race in:"
			// Try to get name of next scheduled race for initial display
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
		_, errActiveRace := getActiveRaceID(db) // Check if a new 'Scheduled' race exists for betting
		isBettingInitiallyOpen = errActiveRace == nil
	} else { // Default / Initializing state
		calculatedTimeStr = "--:--"
		calculatedStatusMsg = "Checking schedule..."
		_, errActiveRace := getActiveRaceID(db)
		isBettingInitiallyOpen = errActiveRace == nil
	}

	data.InitialNextRaceTime = calculatedTimeStr       // For initial template rendering
	data.InitialStatusMessage = calculatedStatusMsg    // For initial template rendering
	data.InitialRaceName = calculatedRaceName          // For initial template rendering
	data.IsBettingInitiallyOpen = isBettingInitiallyOpen // For initial template rendering
	data.CurrentRaceDisplay = pageCurrentRaceDetails   // Still useful for other parts of the template

	err := homeTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("homeHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

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
		// Optional: Show time remaining in current race
		// finishTime := localCurrentRaceDetails.Date.Add(raceDuration) // This Date is start time
		// if time.Now().Before(finishTime) {
		//  countdownStr = fmt.Sprintf("Ends in %s", finishTime.Sub(time.Now()).Round(time.Second))
		// } else {
		//  countdownStr = "Finishing..."
		// }
		isRaceRunning = true
		isBettingOpen = false // Betting closes when race is running
	} else if !localNextRaceStartTime.IsZero() && localNextRaceStartTime.After(time.Now()) {
		durationUntilNext := time.Until(localNextRaceStartTime)
		if durationUntilNext > 0 {
			minutes := int(durationUntilNext.Minutes())
			seconds := int(durationUntilNext.Seconds()) % 60
			countdownStr = fmt.Sprintf("%02d:%02d", minutes, seconds)
			statusMsg = "Next race starts in:"

			// Attempt to get the name of the next scheduled race
			var nextRaceNameDB string
			// The nextRaceStartTime is for *the* next race, find its name
			err := db.QueryRow("SELECT name FROM races WHERE status = ? AND date = ? ORDER BY date ASC LIMIT 1",
				RaceStatusScheduled, localNextRaceStartTime).Scan(&nextRaceNameDB)
			if err == nil {
				raceNameDisplay = nextRaceNameDB
			} else {
				raceNameDisplay = "Upcoming Race"
			}
			isBettingOpen = true // Betting is open for scheduled races
		} else {
			countdownStr = "Starting..."
			statusMsg = "Next race:"
			raceNameDisplay = "Get Ready!"
			isBettingOpen = false // Too close to start, or effectively starting
		}
	} else if localCurrentRaceDetails != nil && localCurrentRaceDetails.Status == RaceStatusFinished {
		statusMsg = "Last race finished:"
		raceNameDisplay = fmt.Sprintf("%s (Winner: %s)", localCurrentRaceDetails.Name, localCurrentRaceDetails.Winner)
		countdownStr = "Next one soon..."
		// Check if a new race has been scheduled yet for betting
        _, err := getActiveRaceID(db) // Check if a new 'Scheduled' race exists
        if err == nil {
            isBettingOpen = true
        } else {
            isBettingOpen = false
        }

	} else {
		countdownStr = "--:--"
		statusMsg = "Checking schedule..."
		raceNameDisplay = "No active race"
		// Check if a new race has been scheduled yet for betting
        _, err := getActiveRaceID(db) // Check if a new 'Scheduled' race exists
        if err == nil {
            isBettingOpen = true
        } else {
            isBettingOpen = false
        }
	}

	data := struct {
		CountdownStr    string
		StatusMsg       string
		RaceName        string
		IsBettingOpen   bool
		IsRaceRunning   bool
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

func loginHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title: "Login - Scramble Run",
	}

	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			log.Printf("loginHandler: Error parsing form: %v", err)
			data.Message = "Error processing form"; data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")

		if email == "" || password == "" {
			data.Message = "Email and password are required"; data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		var storedPasswordHash string
		var userID int
		var userName string
		// var userBalance float64 // If you want to store balance in session

		err = db.QueryRow("SELECT id, name, password_hash FROM users WHERE email = ?", email).Scan(&userID, &userName, &storedPasswordHash)
		// err = db.QueryRow("SELECT id, name, password_hash, balance FROM users WHERE email = ?", email).Scan(&userID, &userName, &storedPasswordHash, &userBalance) // If getting balance
		if err != nil {
			if err == sql.ErrNoRows {
				data.Message = "Invalid email or password"
			} else {
				log.Printf("loginHandler: Database error: %v", err)
				data.Message = "An error occurred. Please try again."
			}
			data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		err = bcrypt.CompareHashAndPassword([]byte(storedPasswordHash), []byte(password))
		if err != nil {
			data.Message = "Invalid email or password" // Keep error message generic
			data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		// Login successful
		// TODO: Implement actual session management (e.g., using gorilla/sessions or similar)
		// Example:
		// session, _ := store.Get(r, "scramble-session")
		// session.Values["userID"] = userID
		// session.Values["userName"] = userName
		// session.Values["userBalance"] = userBalance // Store balance if needed frequently
		// err = session.Save(r, w)
		// if err != nil {
		//    log.Printf("loginHandler: Error saving session: %v", err)
		//    http.Error(w, "Failed to save session", http.StatusInternalServerError)
		//    return
		// }
		log.Printf("User %s (ID: %d) logged in successfully.", userName, userID)

		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	err := loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("loginHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func signupHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title: "Signup - Scramble Run",
	}

	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			log.Printf("signupHandler: Error parsing form: %v", err)
			data.Message = "Error processing form"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		name := r.FormValue("name")
		email := r.FormValue("email")
		password := r.FormValue("password")
		confirmPassword := r.FormValue("confirm_password")

		if name == "" || email == "" || password == "" {
			data.Message = "All fields are required"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if !strings.Contains(email, "@") { // Basic email validation
			data.Message = "Invalid email format"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if len(password) < 6 { // Basic password length check
			data.Message = "Password must be at least 6 characters"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if password != confirmPassword {
			data.Message = "Passwords do not match"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", email).Scan(&count)
		if err != nil {
			log.Printf("signupHandler: Database error checking email: %v", err)
			data.Message = "An error occurred. Please try again."; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if count > 0 {
			data.Message = "Email already in use"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("signupHandler: Error hashing password: %v", err)
			data.Message = "An error occurred. Please try again."; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		// Assumes users table has a 'balance' column with a DEFAULT value (e.g., 1000.0)
		// set in init_database.sql. If not, insert explicitly.
		// For example, `DEFAULT 100.0` in `CREATE TABLE users (... balance REAL DEFAULT 100.0 ...)`
		_, err = db.Exec("INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?)",
			name, email, string(hashedPassword))
		if err != nil {
			log.Printf("signupHandler: Error inserting user: %v", err)
			data.Message = "An error occurred. Please try again."; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		data.Message = "Registration successful! You can now log in."
		data.Success = true
		// It's better to redirect to login after successful signup, or show the message on the login page.
		// For now, rendering login template with the success message.
		_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
		return
	}

	err := signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("signupHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

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
	// Define the template locally for this handler for clarity or use a pre-parsed one
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

func placeBetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	if r.Method != http.MethodPost {
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Method not allowed"})
		return
	}

	currentUserID := 1 // <<< --- !!! PLACEHOLDER: Replace with actual User ID from session !!! --- >>>

	err := r.ParseForm()
	if err != nil {
		log.Printf("placeBetHandler: Failed to parse form: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Error processing request."})
		return
	}

	log.Println("--- placeBetHandler Received Form Data ---")
	for key, values := range r.Form {
		for _, value := range values {
			log.Printf("Form Key: [%s], Value: [%s]\n", key, value)
		}
	}
	log.Println("----------------------------------------")

	betAmountStr := r.FormValue("betAmount")
	log.Printf("placeBetHandler: Raw betAmountStr from form: '%s'", betAmountStr)
	betAmount, err := strconv.ParseFloat(betAmountStr, 64)
	if err != nil || betAmount <= 0 {
		log.Printf("placeBetHandler: Invalid bet amount. String: '%s', Parsed: %f, Error: %v", betAmountStr, betAmount, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Invalid bet amount. Must be a positive number."})
		return
	}

	chickenIDStr := r.FormValue("selectedChicken")
	log.Printf("placeBetHandler: Raw chickenIDStr from form: '%s'", chickenIDStr)
	if chickenIDStr == "" {
		log.Println("placeBetHandler: 'selectedChicken' form value is empty.")
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "No chicken selected. Please select a chicken first."})
		return
	}
	chickenID, err := strconv.Atoi(chickenIDStr)
	if err != nil {
		log.Printf("placeBetHandler: Failed to convert 'selectedChicken' value '%s' to an integer: %v", chickenIDStr, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Invalid chicken selection data. Expected a numeric ID."})
		return
	}
	log.Printf("placeBetHandler: Parsed chickenID: %d", chickenID)

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
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: fmt.Sprintf("The selected chicken (ID: %d) is not available for betting.", chickenID)})
		return
	}
	log.Printf("placeBetHandler: Successfully found chicken: %s (ID: %d)", selectedChicken.Name, selectedChicken.ID)

	tx, err := db.Begin()
	if err != nil {
		log.Printf("placeBetHandler: Failed to begin transaction: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Database error. Please try again later."})
		return
	}
	committed := false
	defer func() {
		if !committed {
			errRollback := tx.Rollback()
			if errRollback != nil {
				log.Printf("placeBetHandler: Error rolling back transaction: %v", errRollback)
			} else {
				log.Println("placeBetHandler: Transaction rolled back due to error.")
			}
		}
	}()

	var currentUserBalance float64
	err = tx.QueryRow("SELECT balance FROM users WHERE id = ?", currentUserID).Scan(&currentUserBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("placeBetHandler: User ID %d not found for betting.", currentUserID)
			_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "User not found."})
		} else {
			log.Printf("placeBetHandler: Error fetching user balance: %v", err)
			_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Database error while fetching balance."})
		}
		return
	}

	if currentUserBalance < betAmount {
		_ = betResponseTemplate.Execute(w, BetResponse{
			Success:     false,
			Message:     fmt.Sprintf("Insufficient funds. Your balance is %.2f credits.", currentUserBalance),
			NewBalance:  currentUserBalance, // Show current balance even on failure
			BetAmount:   betAmount,
			ChickenName: selectedChicken.Name,
		})
		return
	}

	// Get active race ID
	activeRaceID, err := getActiveRaceID(tx) // Use the transaction
	if err != nil {
		log.Printf("placeBetHandler: Could not determine active race: %v", err)
		msg := "Failed to determine active race for betting. Please try again later."
		if strings.Contains(err.Error(), "no active race available") {
			msg = "No races are currently open for betting. Please check back later."
		}
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: msg})
		return
	}
	log.Printf("placeBetHandler: Active Race ID for bet: %d", activeRaceID)


	// Get 'Pending' status ID
	pendingStatusID, err := getPendingBetStatusID(tx) // Use the transaction
	if err != nil {
		log.Printf("placeBetHandler: Could not determine pending bet status: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "System error: Bet status configuration issue."})
		return
	}
	log.Printf("placeBetHandler: Pending Bet Status ID: %d", pendingStatusID)


	newBalance := currentUserBalance - betAmount
	_, err = tx.Exec("UPDATE users SET balance = ? WHERE id = ?", newBalance, currentUserID)
	if err != nil {
		log.Printf("placeBetHandler: Error updating user balance: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to update balance."})
		return
	}


	potentialPayout := betAmount * selectedChicken.Odds // selectedChicken is already fetched

	// Insert the bet with race_id, bet_status_id, and potential_payout
	_, err = tx.Exec("INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id, potential_payout) VALUES (?, ?, ?, ?, ?, ?)",
		currentUserID, activeRaceID, chickenID, betAmount, pendingStatusID, potentialPayout) // Added potentialPayout
	if err != nil {
		log.Printf("placeBetHandler: Error inserting bet: %v", err) // This was the original error point
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to record bet."})
		return
	}
	

	err = tx.Commit()
	if err != nil {
		log.Printf("placeBetHandler: Error committing transaction: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to finalize bet."})
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

	err = betResponseTemplate.Execute(w, response)
	if err != nil {
		log.Printf("placeBetHandler: Failed to render success response: %v", err)
		// Bet was placed, but response failed. Send plain error to client.
		http.Error(w, "Internal Server Error (bet placed, but response failed to render)", http.StatusInternalServerError)
	}
}

func main() {
	if db == nil {
		log.Fatal("Database not initialized (db is nil in main). Exiting.")
		return
	}
	// Defer db.Close() should be after successful opening, init_database handles its own closure on error.
	// If init_database returns a valid db, then we defer its close here.
	defer func() {
		if db != nil {
			log.Println("Closing database connection.")
			db.Close()
		}
	}()


	// Start the race manager goroutine
	go raceLoop(db)

	fs := http.FileServer(http.Dir("src/web/static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/signup", signupHandler)
	http.HandleFunc("/select-chicken/", selectChickenHandler)
	http.HandleFunc("/calculate-winnings", calculateWinningsHandler)
	http.HandleFunc("/place-bet", placeBetHandler)
	http.HandleFunc("/next-race-info", nextRaceInfoHandler)           // HTMX endpoint
	http.HandleFunc("/admin/trigger-race-cycle", handleTriggerRaceCycle) // Dev endpoint

	fmt.Printf("Server starting on http://localhost:%s\n", local_port)
	err := http.ListenAndServe(":"+local_port, nil)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}