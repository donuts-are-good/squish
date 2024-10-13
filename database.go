package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func startDB() (*sqlx.DB, error) {
	dbPath := "irc.db"

	// Check if the database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Println("Database file does not exist. Creating a new one.")
		file, err := os.Create(dbPath)
		if err != nil {
			return nil, fmt.Errorf("error creating database file: %v", err)
		}
		file.Close()
	}

	// Connect to the database
	db, err := sqlx.Connect("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("error connecting to database: %v", err)
	}

	// Create tables
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			nickname TEXT UNIQUE,
			username TEXT,
			hostname TEXT,
			realname TEXT,
			password TEXT,
			invisible BOOLEAN DEFAULT 0,
			is_operator BOOLEAN DEFAULT 0,
			has_voice BOOLEAN DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			is_identified BOOLEAN DEFAULT 0,
			last_seen TIMESTAMP,
			email TEXT
		);

		CREATE TABLE IF NOT EXISTS channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			topic TEXT,
			no_external_messages BOOLEAN DEFAULT 0,
			topic_protection BOOLEAN DEFAULT 0,
			moderated BOOLEAN DEFAULT 0,
			invite_only BOOLEAN DEFAULT 0,
			key TEXT,
			user_limit INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS user_channels (
			user_id INTEGER,
			channel_id INTEGER,
			is_operator BOOLEAN DEFAULT 0,
			has_voice BOOLEAN DEFAULT 0,
			joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, channel_id),
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (channel_id) REFERENCES channels(id)
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("error creating tables: %v", err)
	}

	log.Println("Database initialized successfully")
	return db, nil
}

func getClientByNickname(nickname string) (*Client, error) {
	var client Client
	err := DB.Get(&client, "SELECT id, nickname, username, hostname, realname, password, invisible, is_operator, has_voice, created_at, is_identified, last_seen, email FROM users WHERE nickname = ?", nickname)
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func updateClientInfo(client *Client) error {
	_, err := DB.Exec(`
		UPDATE users 
		SET username = ?, hostname = ?, realname = ?, password = ?, last_seen = ?, email = ?
		WHERE nickname = ?
	`, client.Username, client.Hostname, client.Realname, client.Password, client.LastSeen, client.Email, client.Nickname)
	return err
}

func createClient(client *Client) error {
	result, err := DB.Exec(`
		INSERT INTO users (nickname, username, hostname, realname, password, created_at, last_seen, email)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, client.Nickname, client.Username, client.Hostname, client.Realname, client.Password, client.CreatedAt, client.LastSeen, client.Email)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	client.ID = id
	return nil
}

func getOrCreateChannel(name string) (*Channel, error) {
	var channel Channel
	err := DB.Get(&channel, "SELECT * FROM channels WHERE name = ?", name)
	if err == nil {
		return &channel, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	result, err := DB.Exec(`
		INSERT INTO channels (name, topic)
		VALUES (?, ?)
	`, name, "Welcome to "+name)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	channel = Channel{
		ID:    id,
		Name:  name,
		Topic: "Welcome to " + name,
		Key:   sql.NullString{String: "", Valid: false},
	}
	return &channel, nil
}

func addClientToChannel(client *Client, channel *Channel) error {
	_, err := DB.Exec(`
		INSERT INTO user_channels (user_id, channel_id)
		VALUES (?, ?)
	`, client.ID, channel.ID)
	return err
}

func removeClientFromChannel(client *Client, channel *Channel) error {
	// Remove from in-memory structure
	for i, c := range channel.Clients {
		if c.conn == client.conn {
			channel.Clients = append(channel.Clients[:i], channel.Clients[i+1:]...)
			break
		}
	}

	// Remove from database
	_, err := DB.Exec(`
		DELETE FROM user_channels
		WHERE user_id = ? AND channel_id = ?
	`, client.ID, channel.ID)
	return err
}

func getChannelsForClient(client *Client) ([]*Channel, error) {
	var channels []*Channel
	err := DB.Select(&channels, `
		SELECT c.*
		FROM channels c
		JOIN user_channels uc ON c.id = uc.channel_id
		WHERE uc.user_id = ?
	`, client.ID)
	return channels, err
}

func getClientsInChannel(channel *Channel) ([]*Client, error) {
	var clients []*Client
	err := DB.Select(&clients, `
		SELECT u.*
		FROM users u
		JOIN user_channels uc ON u.id = uc.user_id
		WHERE uc.channel_id = ?
	`, channel.ID)
	return clients, err
}

func syncInMemoryState() {
	mu.Lock()
	defer mu.Unlock()

	for _, client := range clients {
		channels, err := getChannelsForClient(client)
		if err != nil {
			log.Printf("Error syncing channels for client %s: %v", client.Nickname, err)
		} else {
			client.Channels = channels
		}
	}

	for _, channel := range channels {
		clients, err := getClientsInChannel(channel)
		if err != nil {
			log.Printf("Error syncing clients for channel %s: %v", channel.Name, err)
		} else {
			channel.Clients = clients
		}
	}
}

func getAllChannels() ([]*Channel, error) {
	var channels []*Channel
	err := DB.Select(&channels, "SELECT * FROM channels")
	return channels, err
}

func getChannelUserCount(channelID int64) (int, error) {
	var count int
	err := DB.Get(&count, "SELECT COUNT(*) FROM user_channels WHERE channel_id = ?", channelID)
	return count, err
}

func updateClientNickname(client *Client) error {
	_, err := DB.Exec("UPDATE users SET nickname = ? WHERE id = ?", client.Nickname, client.ID)
	return err
}

func createOrUpdateClient(client *Client, password string) error {
	var err error
	existingClient, err := getClientByNickname(client.Nickname)
	if err == sql.ErrNoRows {
		// New client, create a new database entry
		err = createClient(client)
	} else if err == nil {
		// Existing client, update the database entry
		client.ID = existingClient.ID
		client.Password = password
		err = updateClientInfo(client)
	} else {
		return err
	}
	return err
}
