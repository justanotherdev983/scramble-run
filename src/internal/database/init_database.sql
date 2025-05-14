-- Drop tables if they exist to start fresh
DROP TABLE IF EXISTS bets;
DROP TABLE IF EXISTS races;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS chickens; -- Drop chickens before tables that reference it
DROP TABLE IF EXISTS bet_statuses;

-- Chickens Table
CREATE TABLE IF NOT EXISTS chickens (
                                        id INTEGER PRIMARY KEY AUTOINCREMENT,
                                        name TEXT NOT NULL UNIQUE,
                                        odds REAL NOT NULL DEFAULT 2.0 CHECK (odds >= 1.0), -- Odds for the chicken, e.g., 2.0 means 2:1
    -- You can add other chicken-specific attributes here (e.g., color, breed, image_url)
                                        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Users Table
CREATE TABLE IF NOT EXISTS users (
                                     id INTEGER PRIMARY KEY AUTOINCREMENT,
                                     name TEXT NOT NULL,
                                     email TEXT UNIQUE NOT NULL,
                                     password_hash TEXT NOT NULL,
                                     balance REAL DEFAULT 1000.0 NOT NULL,
                                     created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                     updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Races Table
CREATE TABLE IF NOT EXISTS races (
                                     id INTEGER PRIMARY KEY AUTOINCREMENT,
                                     name TEXT NOT NULL,
                                     date TIMESTAMP NOT NULL,       -- Scheduled start time of the race
                                     winner_chicken_id INTEGER,     -- FK to chickens table (ID of the winning chicken)
                                     winner TEXT,                   -- Name of the winning chicken (can be derived or stored)
                                     status TEXT NOT NULL DEFAULT 'Scheduled', -- 'Scheduled', 'Running', 'Finished', 'Cancelled'
                                     created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                     updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                     FOREIGN KEY (winner_chicken_id) REFERENCES chickens(id)
);

-- Bet Statuses Table
CREATE TABLE IF NOT EXISTS bet_statuses (
                                            id INTEGER PRIMARY KEY AUTOINCREMENT,
                                            status_name TEXT NOT NULL UNIQUE
);

-- Bets Table
CREATE TABLE IF NOT EXISTS bets (
                                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                                    user_id INTEGER NOT NULL,
                                    race_id INTEGER NOT NULL,
                                    chicken_id INTEGER NOT NULL,
                                    bet_amount REAL NOT NULL CHECK (bet_amount > 0),
                                    bet_status_id INTEGER NOT NULL,
                                    potential_payout REAL, 
                                    actual_payout REAL DEFAULT 0,
                                    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                    FOREIGN KEY (user_id) REFERENCES users (id),
                                    FOREIGN KEY (race_id) REFERENCES races (id),
                                    FOREIGN KEY (chicken_id) REFERENCES chickens (id),
                                    FOREIGN KEY (bet_status_id) REFERENCES bet_statuses (id)
);

CREATE TABLE IF NOT EXISTS contact_messages
(
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    topic      VARCHAR(255) NOT NULL,
    email      VARCHAR(255) NOT NULL,
    message    TEXT         NOT NULL,
    created_at TIMESTAMP    NOT NULL,
    ip_address VARCHAR(45)  NOT NULL
);

-- Insert sample data for bet_statuses
-- Make sure these align with what your Go code expects (Pending, Won, Lost)
INSERT INTO bet_statuses (status_name) VALUES ('Pending'), ('Won'), ('Lost'), ('Cancelled');
-- The old 'Completed' might be ambiguous; 'Won'/'Lost' are more specific for betting.

-- Insert sample data for users
INSERT INTO users (name, email, password_hash, balance) VALUES
                                                            ('John Doe', 'john.doe@example.com', '$2a$10$abcdefghijklmnopqrstuvwx', 1000.0),
                                                            ('Jane Smith', 'jane.smith@example.com', '$2a$10$zyxwvutsrqponmlkjihgfedcb', 1000.0);

-- Insert sample data for chickens
-- Ensure availableChickens in your Go code can be populated from this or matches this
INSERT INTO chickens (name, odds) VALUES
                                      ('Henrietta', 2.5),          -- ID 1
                                      ('Cluck Norris', 1.8),       -- ID 2
                                      ('Foghorn Leghorn Jr.', 3.0), -- ID 3
                                      ('The Eggsecutioner', 4.5),  -- ID 4
                                      ('Speedy Gonzales', 2.2);    -- ID 5 (just kidding, it's a chicken race!)

-- Insert sample data for races (past/completed examples)
-- Assume bet_statuses: Pending=1, Won=2, Lost=3, Cancelled=4 (based on insertion order)
-- For past races, winner_chicken_id should be set.
INSERT INTO races (name, date, winner_chicken_id, winner, status) VALUES
                                                                      ('The Grand Cluck Off', '2025-01-25 10:00:00', 1, 'Henrietta', 'Finished'),
                                                                      ('Feathered Fury Derby', '2025-02-14 15:30:00', 2, 'Cluck Norris', 'Finished');

-- Add a new race that is open for betting (no winner yet, future date)
INSERT INTO races (name, date, status) VALUES
    ('Upcoming Eggstravaganza', '2025-06-01 14:00:00', 'Scheduled');

-- Insert sample data for bets
-- User 1 (John Doe) bet on Henrietta (Chicken ID 1) for Race 1 (The Grand Cluck Off). Henrietta won.
INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id, actual_payout)
VALUES (1, 1, 1, 50.0, (SELECT id FROM bet_statuses WHERE status_name = 'Won'), 50.0 * (SELECT odds FROM chickens WHERE id = 1) ); -- Payout = bet * odds

-- User 2 (Jane Smith) bet on Foghorn (Chicken ID 3) for Race 2 (Feathered Fury Derby). Foghorn lost to Cluck Norris.
INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id, actual_payout)
VALUES (2, 2, 3, 25.0, (SELECT id FROM bet_statuses WHERE status_name = 'Lost'), 0.0);

-- Example of a pending bet for the "Upcoming Eggstravaganza" race (Race ID 3)
-- User 1 (John Doe) bets on Cluck Norris (Chicken ID 2) for Race 3. (Status: Pending)
INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id)
VALUES (1, 3, 2, 100.0, (SELECT id FROM bet_statuses WHERE status_name = 'Pending'));


-- Create indexes
CREATE INDEX IF NOT EXISTS idx_bets_user_id ON bets (user_id);
CREATE INDEX IF NOT EXISTS idx_bets_race_id ON bets (race_id);
CREATE INDEX IF NOT EXISTS idx_bets_chicken_id ON bets (chicken_id); -- Index for chicken_id in bets
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);
CREATE INDEX IF NOT EXISTS idx_races_status_date ON races (status, date); -- Useful for finding races to start/bet on
CREATE INDEX IF NOT EXISTS idx_chickens_name ON chickens (name);