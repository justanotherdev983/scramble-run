# Nulde Normaalvorm (0NF)

| Field         | Value                           |  
|---------------|---------------------------------|  
| id            | 1                               |  
| name          | John Doe                        |  
| email         | john.doe@example.com            |  
| race_id       | 1001                            |  
| race_name     | Chicken Race 1                  |  
| race_winner   | Chicken A                       |  
| race_date     | 2025-01-25                      |  
| chicken_names | Chicken A, Chicken B, Chicken C |  
| bet_amount    | 50                              |  
| bet_winner    | Chicken A                       |  
| bet_status    | Completed                       |  
  
---  

# Eerste Normaalvorm (1NF)

## Users Table


CREATE TABLE IF NOT EXISTS users (  
id INTEGER PRIMARY KEY AUTOINCREMENT,  
name TEXT NOT NULL,  
email TEXT UNIQUE NOT NULL  
);  
Races Table



CREATE TABLE IF NOT EXISTS races (  
id INTEGER PRIMARY KEY AUTOINCREMENT,  
name TEXT NOT NULL,  
winner TEXT NOT NULL,  
date TIMESTAMP DEFAULT CURRENT_TIMESTAMP  
);  
Bets Table



CREATE TABLE IF NOT EXISTS bets (  
id INTEGER PRIMARY KEY AUTOINCREMENT,  
user_id INTEGER,  
race_id INTEGER,  
bet_amount INTEGER NOT NULL,  
bet_winner TEXT,  
bet_status TEXT,  
FOREIGN KEY(user_id) REFERENCES users(id),  
FOREIGN KEY(race_id) REFERENCES races(id)  
);  
Tweede Normaalvorm (2NF)  
Users Table (No change from 1NF)  
Races Table (No change from 1NF)  
Bets Table (Updated)



CREATE TABLE IF NOT EXISTS bets (  
id INTEGER PRIMARY KEY AUTOINCREMENT,  
user_id INTEGER,  
race_id INTEGER,  
bet_amount INTEGER NOT NULL,  
bet_status TEXT,  
FOREIGN KEY(user_id) REFERENCES users(id),  
FOREIGN KEY(race_id) REFERENCES races(id)  
);  
Race Winners Table  
sql  
Kopiëren  
Bewerken  
CREATE TABLE IF NOT EXISTS race_winners (  
race_id INTEGER PRIMARY KEY,  
winner TEXT NOT NULL,  
FOREIGN KEY(race_id) REFERENCES races(id)  
);  
Derde Normaalvorm (3NF)  
Users Table (No change from 2NF)  
Races Table (No change from 2NF)  
Bets Table (Updated)

CREATE TABLE IF NOT EXISTS bets (  
id INTEGER PRIMARY KEY AUTOINCREMENT,  
user_id INTEGER,  
race_id INTEGER,  
bet_amount INTEGER NOT NULL,  
bet_status_id INTEGER,  
FOREIGN KEY(user_id) REFERENCES users(id),  
FOREIGN KEY(race_id) REFERENCES races(id),  
FOREIGN KEY(bet_status_id) REFERENCES bet_statuses(id)  
);  
Bet Statuses Table

CREATE TABLE IF NOT EXISTS bet_statuses (  
id INTEGER PRIMARY KEY AUTOINCREMENT,  
status_name TEXT NOT NULL  
);  
Relaties tussen de Tabellen  
Users and Bets are related by user_id.  
Races and Bets are related by race_id.  
Races and Race Winners are related by race_id.  
Bets and Bet Statuses are related by bet_status_id.


This markdown organizes the normalization steps into different sections with SQL table creation statements for each form of normalization.


# Real 

-- Users Table (3NF)
CREATE TABLE IF NOT EXISTS users (
id INTEGER PRIMARY KEY AUTOINCREMENT,
name TEXT NOT NULL,
email TEXT UNIQUE NOT NULL,
password_hash TEXT NOT NULL, -- Store hashed passwords
created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Races Table (3NF)
CREATE TABLE IF NOT EXISTS races (
id INTEGER PRIMARY KEY AUTOINCREMENT,
name TEXT NOT NULL,
date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
winner TEXT, -- Winner is now in the races table
created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Bet Statuses Table (3NF)
CREATE TABLE IF NOT EXISTS bet_statuses (
id INTEGER PRIMARY KEY AUTOINCREMENT,
status_name TEXT NOT NULL
);

-- Bets Table (3NF)
CREATE TABLE IF NOT EXISTS bets (
id INTEGER PRIMARY KEY AUTOINCREMENT,
user_id INTEGER,
race_id INTEGER,
bet_amount INTEGER NOT NULL CHECK (bet_amount > 0), -- Ensure bet_amount is positive
bet_status_id INTEGER,
bet_winner TEXT,
FOREIGN KEY(user_id) REFERENCES users(id),
FOREIGN KEY(race_id) REFERENCES races(id),
FOREIGN KEY(bet_status_id) REFERENCES bet_statuses(id),
created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);