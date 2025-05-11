package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// init_database initializes and returns a database connection.
// It also checks if the schema needs to be created/updated.
func init_database() *sql.DB {
	db, err := sql.Open("sqlite3", "src/internal/database/scramble.db")
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return nil
	}

	shouldInitialize := false
	var tableCount int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('users', 'races', 'chickens', 'bets', 'bet_statuses');").Scan(&tableCount)
	if err != nil {
		log.Printf("Failed to check for core tables: %v. Assuming initialization is needed.", err)
		shouldInitialize = true
	} else if tableCount < 5 {
		log.Printf("Found %d core tables, expected 5. Database might be incomplete. Attempting initialization.", tableCount)
		shouldInitialize = true
	} else {
		var balanceColExists int
		err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='balance'").Scan(&balanceColExists)
		if err != nil || balanceColExists == 0 {
			log.Println("'balance' column not found in 'users' or error checking. Database might need re-initialization.")
			shouldInitialize = true
		}
		var statusColExistsRaces int
		err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('races') WHERE name='status'").Scan(&statusColExistsRaces)
		if err != nil || statusColExistsRaces == 0 {
			log.Println("'status' column not found in 'races' or error checking. Database might need re-initialization.")
			shouldInitialize = true
		}
	}

	if shouldInitialize {
		log.Println("Attempting to initialize database from SQL file...")
		sqlFile, errRead := os.ReadFile("src/internal/database/init_database.sql")
		if errRead != nil {
			log.Printf("Failed to read SQL initialization file: %v", errRead)
			db.Close()
			return nil
		}
		_, errExec := db.Exec(string(sqlFile))
		if errExec != nil {
			log.Printf("Failed to initialize the database: %v", errExec)
			db.Close()
			return nil
		}
		fmt.Println("Database initialized/verified successfully from SQL file.")
	} else {
		fmt.Println("Database structure appears up-to-date.")
	}
	return db
}

// parseRaceDate attempts to parse a date string from the database using various common formats.
func parseRaceDate(dateStr string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	var parsedTime time.Time
	var err error
	for _, layout := range layouts {
		parsedTime, err = time.ParseInLocation(layout, dateStr, time.Local)
		if err == nil {
			return parsedTime, nil
		}
	}
	return time.Time{}, fmt.Errorf("failed to parse date string '%s': %w", dateStr, err)
}

// getRaceDetails fetches detailed information for a single race.
func getRaceDetails(querier rowQuerier, raceID int) (*RaceInfo, error) {
	var race RaceInfo
	var dateStr string
	var winnerID sql.NullInt64
	var winnerName sql.NullString

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

	parsedTime, errParse := parseRaceDate(dateStr)
	if errParse != nil {
		log.Printf("getRaceDetails: Failed to parse date '%s' for race ID %d: %v", dateStr, race.Id, errParse)
		race.Date = time.Time{}
	} else {
		race.Date = parsedTime
	}

	race.WinnerChickenID = winnerID
	if winnerName.Valid {
		race.Winner = winnerName.String
	} else if winnerID.Valid {
		race.Winner = fmt.Sprintf("Chicken ID %d (name unknown)", winnerID.Int64)
	} else {
		race.Winner = "N/A"
	}

	race.ChickenNames = make([]string, len(availableChickens))
	for i, ch := range availableChickens {
		race.ChickenNames[i] = ch.Name
	}
	return &race, nil
}

// getActiveRaceID identifies the race that is currently open for betting.
func getActiveRaceID(dbq rowQuerier) (int, error) {
	var raceID int
	err := dbq.QueryRow("SELECT id FROM races WHERE status = ? ORDER BY date ASC LIMIT 1", RaceStatusScheduled).Scan(&raceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("no race currently scheduled and open for betting")
		}
		return 0, fmt.Errorf("error fetching active race ID for betting: %w", err)
	}
	return raceID, nil
}

// getPendingBetStatusID retrieves the ID for the 'Pending' bet status.
func getPendingBetStatusID(dbq rowQuerier) (int, error) {
	var statusID int
	err := dbq.QueryRow("SELECT id FROM bet_statuses WHERE status_name = 'Pending'").Scan(&statusID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("bet status 'Pending' not found in database; please ensure bet_statuses table is initialized correctly")
		}
		return 0, fmt.Errorf("error fetching 'Pending' bet status ID: %w", err)
	}
	return statusID, nil
}

// get_races fetches a list of all races, ordered by date descending.
func get_races(db *sql.DB) []RaceInfo {
	rows, err := db.Query(`
        SELECT r.id, r.name, r.date, r.status, r.winner_chicken_id, c.name AS winner_name
        FROM races r
        LEFT JOIN chickens c ON r.winner_chicken_id = c.id
        ORDER BY r.date DESC
    `)
	if err != nil {
		log.Printf("get_races: Failed to query races: %v", err)
		return nil
	}
	defer rows.Close()

	var races []RaceInfo
	for rows.Next() {
		var race RaceInfo
		var dateStr string
		var winnerID sql.NullInt64
		var winnerName sql.NullString

		err = rows.Scan(&race.Id, &race.Name, &dateStr, &race.Status, &winnerID, &winnerName)
		if err != nil {
			log.Printf("get_races: Failed to scan row: %v", err)
			continue
		}

		parsedTime, errParse := parseRaceDate(dateStr)
		if errParse != nil {
			log.Printf("get_races: Failed to parse date string '%s' for race ID %d: %v", dateStr, race.Id, errParse)
			race.Date = time.Time{}
		} else {
			race.Date = parsedTime
		}

		race.WinnerChickenID = winnerID
		if winnerName.Valid {
			race.Winner = winnerName.String
		} else if winnerID.Valid {
			race.Winner = fmt.Sprintf("Chicken ID %d", winnerID.Int64)
		} else if race.Status == RaceStatusFinished {
			race.Winner = "N/A (Winner not recorded)"
		} else {
			race.Winner = ""
		}
		races = append(races, race)
	}
	if err := rows.Err(); err != nil {
		log.Printf("get_races: Error iterating through rows: %v", err)
	}
	return races
}