package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

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
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			is_registered BOOLEAN DEFAULT 0,
			founder_id INTEGER,
			FOREIGN KEY (founder_id) REFERENCES users(id)
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

		CREATE TABLE IF NOT EXISTS channel_bans (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER,
			mask TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
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
	query := `
		SELECT id, nickname, username, 
			   COALESCE(hostname, '') as hostname, 
			   COALESCE(realname, '') as realname, 
			   COALESCE(password, '') as password, 
			   invisible, is_operator, has_voice, created_at, 
			   is_identified, last_seen, 
			   COALESCE(email, '') as email 
		FROM users 
		WHERE nickname = ?
	`
	err := DB.Get(&client, query, nickname)
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func updateClientInfo(client *Client) error {
	_, err := DB.Exec(`
		UPDATE users 
		SET username = ?, hostname = ?, realname = ?, password = ?, last_seen = ?, email = ?, is_identified = ?
		WHERE id = ?
	`, client.Username, client.Hostname, client.Realname, client.Password, client.LastSeen, client.Email, client.IsIdentified, client.ID)
	return err
}

func createClient(client *Client) error {
	result, err := DB.Exec(`
		INSERT INTO users (nickname, username, hostname, realname, created_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)
	`, client.Nickname, client.Username, client.Hostname, client.Realname, client.CreatedAt, client.LastSeen)
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
	// Reject attempts to create a channel named "na"
	if strings.EqualFold(name, "#na") {
		return nil, fmt.Errorf("invalid channel name: %s", name)
	}

	var channel Channel
	err := DB.Get(&channel, "SELECT * FROM channels WHERE name = ?", name)
	if err == nil {
		// Channel exists, fetch its clients
		channel.Clients, err = getClientsInChannel(&channel)
		if err != nil {
			return nil, fmt.Errorf("error fetching clients for channel %s: %v", name, err)
		}
		return &channel, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// Channel doesn't exist, create it
	log.Printf("Creating new channel: %s", name)
	result, err := DB.Exec(`
		INSERT INTO channels (name, topic, created_at)
		VALUES (?, ?, ?)
	`, name, "Welcome to "+name, time.Now())
	if err != nil {
		return nil, fmt.Errorf("error creating channel %s: %v", name, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("error getting last insert ID for channel %s: %v", name, err)
	}

	channel = Channel{
		ID:        id,
		Name:      name,
		Topic:     "Welcome to " + name,
		Key:       sql.NullString{String: "", Valid: false},
		CreatedAt: time.Now(),
		Clients:   []*Client{},
	}
	return &channel, nil
}

func addClientToChannel(client *Client, channel *Channel, isOperator bool) error {
	_, err := DB.Exec(`
		INSERT INTO user_channels (user_id, channel_id, is_operator)
		VALUES (?, ?, ?)
	`, client.ID, channel.ID, isOperator)
	if err != nil {
		return err
	}

	// Update the client's IsOperator status
	if isOperator {
		client.IsOperator = true
	}

	return nil
}

func removeClientFromChannel(client *Client, channel *Channel) error {
	log.Printf("Attempting to remove client %s from channel %s", client.Nickname, channel.Name)
	// Remove from database
	_, err := DB.Exec(`
		DELETE FROM user_channels
		WHERE user_id = ? AND channel_id = ?
	`, client.ID, channel.ID)
	if err != nil {
		log.Printf("Error removing client %s from channel %s in database: %v", client.Nickname, channel.Name, err)
		return err
	}
	log.Printf("Successfully removed client %s from channel %s in database", client.Nickname, channel.Name)

	// Remove the channel from the client's list of channels
	for i, ch := range client.Channels {
		if ch.ID == channel.ID {
			client.Channels = append(client.Channels[:i], client.Channels[i+1:]...)
			log.Printf("Removed channel %s from client %s's channel list", channel.Name, client.Nickname)
			break
		}
	}

	return nil
}

func getClientsInChannel(channel *Channel) ([]*Client, error) {
	var clients []*Client
	query := `
		SELECT u.id, u.nickname, u.username, 
			   COALESCE(u.hostname, '') as hostname, 
			   COALESCE(u.realname, '') as realname, 
			   u.invisible, u.is_operator, u.has_voice, u.created_at, 
			   u.is_identified, u.last_seen, 
			   COALESCE(u.email, '') as email
		FROM users u
		JOIN user_channels uc ON u.id = uc.user_id
		WHERE uc.channel_id = ?
	`
	err := DB.Select(&clients, query, channel.ID)
	return clients, err
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

func isClientInChannel(client *Client, channel *Channel) (bool, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM user_channels WHERE user_id = ? AND channel_id = ?", client.ID, channel.ID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func getChannel(name string) (*Channel, error) {
	var channel Channel
	err := DB.Get(&channel, "SELECT * FROM channels WHERE name = ?", name)
	if err != nil {
		return nil, err
	}
	return &channel, nil
}

func addChannelBan(channelID int64, mask string) error {
	_, err := DB.Exec(`
		INSERT INTO channel_bans (channel_id, mask)
		VALUES (?, ?)
	`, channelID, mask)
	return err
}

func removeChannelBan(channelID int64, mask string) error {
	_, err := DB.Exec(`
		DELETE FROM channel_bans
		WHERE channel_id = ? AND mask = ?
	`, channelID, mask)
	return err
}

func isClientBanned(client *Client, channel *Channel) (bool, error) {
	var banned bool
	err := DB.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM channel_bans
			WHERE channel_id = ? AND (
				? LIKE mask OR
				? LIKE mask OR
				? LIKE mask
			)
		)
	`, channel.ID,
		client.Nickname+"!"+client.Username+"@"+client.Hostname,
		client.Username+"@"+client.Hostname,
		client.Hostname).Scan(&banned)
	return banned, err
}

func getChannelBans(channelID int64) ([]string, error) {
	var bans []string
	err := DB.Select(&bans, "SELECT mask FROM channel_bans WHERE channel_id = ?", channelID)
	return bans, err
}
