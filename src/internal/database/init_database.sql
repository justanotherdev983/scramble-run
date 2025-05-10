-- Drop tables if they exist to start fresh
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS races;
DROP TABLE IF EXISTS bets;
DROP TABLE IF EXISTS bet_statuses;

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
    date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    winner TEXT, 
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
    bet_winner TEXT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users (id),
    FOREIGN KEY (race_id) REFERENCES races (id),
    FOREIGN KEY (bet_status_id) REFERENCES bet_statuses (id)
);

-- Insert sample data for bet_statuses
INSERT INTO bet_statuses (status_name) VALUES ('Pending'), ('Completed'), ('Cancelled'); -- Pending will be ID 1

-- Insert sample data for users
INSERT INTO users (name, email, password_hash) VALUES
('John Doe', 'john.doe@example.com', '$2a$10$abcdefghijklmnopqrstuvwx'),
('Jane Smith', 'jane.smith@example.com', '$2a$10$zyxwvutsrqponmlkjihgfedcb');

-- Insert sample data for races
INSERT INTO races (name, date, winner) VALUES
('The Grand Cluck Off', '2025-01-25 10:00:00', 'Henrietta'),       -- ID 1 (Completed)
('Feathered Fury Derby', '2025-02-14 15:30:00', 'Cluck Norris');  -- ID 2 (Completed)

-- Add a new race that is open for betting (no winner yet, future date)
INSERT INTO races (name, date) VALUES
('Upcoming Eggstravaganza', '2025-06-01 14:00:00'); -- ID will be 3 (Open for betting)


-- Insert sample data for bets
-- Assumes chicken IDs: 1: Henrietta, 2: Cluck Norris, 3: Foghorn
-- Assumes bet status IDs: 1: Pending, 2: Completed, 3: Cancelled

-- User 1 (John Doe) bet on Henrietta (ID 1) for Race 1. Henrietta won. (Status: Completed)
INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id, bet_winner)
VALUES (1, 1, 1, 50.0, 2, 'Henrietta');

-- User 2 (Jane Smith) bet on Foghorn (ID 3) for Race 2. Foghorn lost. (Status: Completed)
INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id, bet_winner)
VALUES (2, 2, 3, 25.0, 2, NULL);

-- Example of a pending bet for the new "Upcoming Eggstravaganza" race (Race ID 3)
-- User 1 (John Doe) bets on Cluck Norris (Chicken ID 2) for Race 3. (Status: Pending)
INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id)
VALUES (1, 3, 2, 100.0, 1);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_bets_user_id ON bets (user_id);
CREATE INDEX IF NOT EXISTS idx_bets_race_id ON bets (race_id);
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);