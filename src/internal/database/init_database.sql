-- init.db

-- Drop tables if they exist to start fresh
DROP TABLE IF EXISTS users;

DROP TABLE IF EXISTS races;

DROP TABLE IF EXISTS bets;

DROP TABLE IF EXISTS bet_statuses;

-- Users Table (3NF)
CREATE TABLE
    IF NOT EXISTS users (
                            id INTEGER PRIMARY KEY AUTOINCREMENT,
                            name TEXT NOT NULL,
                            email TEXT UNIQUE NOT NULL,
                            password_hash TEXT NOT NULL,
    -- Store hashed passwords
                            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Races Table (3NF)
CREATE TABLE
    IF NOT EXISTS races (
                            id INTEGER PRIMARY KEY AUTOINCREMENT,
                            name TEXT NOT NULL,
                            date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                            winner TEXT,
    -- Winner is now in the races table
                            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Bet Statuses Table (3NF)
CREATE TABLE
    IF NOT EXISTS bet_statuses (
                                   id INTEGER PRIMARY KEY AUTOINCREMENT,
                                   status_name TEXT NOT NULL
);

-- Bets Table (3NF)
CREATE TABLE
    IF NOT EXISTS bets (
                           id INTEGER PRIMARY KEY AUTOINCREMENT,
                           user_id INTEGER,
                           race_id INTEGER,
                           bet_amount INTEGER NOT NULL CHECK (bet_amount > 0),
    -- Ensure bet_amount is positive
                           bet_status_id INTEGER,
                           bet_winner TEXT,
                           created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                           updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                           FOREIGN KEY (user_id) REFERENCES users (id),
                           FOREIGN KEY (race_id) REFERENCES races (id),
                           FOREIGN KEY (bet_status_id) REFERENCES bet_statuses (id)
);

-- Insert some sample data for bet_statuses
INSERT INTO
    bet_statuses (status_name)
VALUES
    ('Pending'),
    ('Completed'),
    ('Cancelled');

-- Insert some sample data for users (replace with hashed passwords!)
INSERT INTO
    users (name, email, password_hash)
VALUES
    (
        'John Doe',
        'john.doe@example.com',
        '$2a$10$abcdefghijklmnopqrstuvwx'
    ),
    (
        'Jane Smith',
        'jane.smith@example.com',
        '$2a$10$zyxwvutsrqponmlkjihgfedcb'
    );

-- Insert some sample data for races
INSERT INTO
    races (name, date, winner)
VALUES
    ('Chicken Race 1', '2025-01-25', 'Chicken A'),
    ('Chicken Race 2', '2025-02-14', 'Chicken B');

-- Insert some sample data for bets
INSERT INTO
    bets (user_id, race_id, bet_amount, bet_status_id, bet_winner)
VALUES
    (1, 1, 50, 2, 'Chicken A'),
    (2, 2, 25, 1, 'Chicken C');

-- Create indexes (important for performance)
CREATE INDEX idx_bets_user_id ON bets (user_id);

CREATE INDEX idx_bets_race_id ON bets (race_id);
