package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

const ServerNameString = "SquishIRC"
const ServerVersionString = "v0.1.1"

var startTime = time.Now()

type Client struct {
	conn         net.Conn   `db:"-" json:"-"`
	ID           int64      `db:"id" json:"id"`
	Nickname     string     `db:"nickname" json:"nickname"`
	Username     string     `db:"username" json:"username"`
	Hostname     string     `db:"hostname" json:"hostname"`
	Realname     string     `db:"realname" json:"realname"`
	Password     string     `db:"password" json:"-"`
	Channels     []*Channel `db:"-" json:"channels,omitempty"`
	Invisible    bool       `db:"invisible" json:"invisible"`
	IsOperator   bool       `db:"is_operator" json:"is_operator"`
	HasVoice     bool       `db:"has_voice" json:"has_voice"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	IsIdentified bool       `db:"is_identified" json:"is_identified"`
	LastSeen     time.Time  `db:"last_seen" json:"last_seen"`
	Email        string     `db:"email" json:"email"`
}

type Channel struct {
	ID                 int64          `db:"id" json:"id"`
	Name               string         `db:"name" json:"name"`
	Topic              string         `db:"topic" json:"topic"`
	Clients            []*Client      `db:"-" json:"clients,omitempty"`
	NoExternalMessages bool           `db:"no_external_messages" json:"no_external_messages"`
	TopicProtection    bool           `db:"topic_protection" json:"topic_protection"`
	Moderated          bool           `db:"moderated" json:"moderated"`
	InviteOnly         bool           `db:"invite_only" json:"invite_only"`
	Key                sql.NullString `db:"key" json:"key"`
	UserLimit          int            `db:"user_limit" json:"user_limit"`
	CreatedAt          time.Time      `db:"created_at" json:"created_at"`
}

var (
	clients  []*Client
	channels []*Channel
	mu       sync.Mutex
	DB       *sqlx.DB
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
			email TEXT,
			is_identified BOOLEAN DEFAULT 0,
			last_seen TIMESTAMP
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

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic in main: %v", r)
		}
	}()

	log.Println("Starting Squish on 6667")
	ln, err := net.Listen("tcp", ":6667")
	if err != nil {
		log.Fatalln(err)
	}
	defer ln.Close()

	DB, err = startDB()
	if err != nil {
		log.Fatalf("Failed to start database: %v", err)
	}
	defer DB.Close()

	// Create default channels
	defaultChannels := []string{"#general", "#help", "#off-topic"}
	for _, channelName := range defaultChannels {
		_, err := getOrCreateChannel(channelName)
		if err != nil {
			log.Printf("Error creating default channel %s: %v", channelName, err)
		}
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			syncInMemoryState()
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("conn: client connected:", conn.RemoteAddr())

		go handleConnection(conn)
	}
}

func broadcastMessage(channel *Channel, sender *Client, message string) {
	for _, client := range channel.Clients {
		if client != sender {
			client.conn.Write([]byte(fmt.Sprintf(":%s PRIVMSG %s %s\r\n", sender.Nickname, channel.Name, message)))
		}
	}
}

func sendMotd(client *Client) {
	motdPath := "server.motd.txt"
	motdBytes, err := os.ReadFile(motdPath)
	if err != nil {
		log.Printf("Failed to read MOTD file: %v", err)
		return
	}
	motdContent := string(motdBytes)

	lines := strings.Split(motdContent, "\n")
	for _, line := range lines {
		if line != "" {
			client.conn.Write([]byte(fmt.Sprintf(":%s 372 %s :- %s\r\n", ServerNameString, client.Nickname, line)))
		}
	}

	client.conn.Write([]byte(fmt.Sprintf(":%s 376 %s :End of /MOTD command.\r\n", ServerNameString, client.Nickname)))
}

func sendWelcomeMessages(client *Client) {
	log.Printf("Sending welcome messages to %s", client.Nickname)
	startTimeStr := startTime.Format(time.RFC3339)
	messages := []string{
		fmt.Sprintf(":%s 001 %s :Welcome to SquishIRC\r\n", ServerNameString, client.Nickname),
		fmt.Sprintf(":%s 002 %s :Your host is %s, running version "+ServerVersionString+"\r\n", ServerNameString, client.Nickname, ServerNameString),
		fmt.Sprintf(":%s 003 %s :This server achieved liftoff on %s\r\n", ServerNameString, client.Nickname, startTimeStr),
		fmt.Sprintf(":%s 004 %s %s "+ServerVersionString+" o o\r\n", ServerNameString, client.Nickname, ServerNameString),
	}

	for _, msg := range messages {
		_, err := client.conn.Write([]byte(msg))
		if err != nil {
			log.Printf("Error sending welcome message to %s: %v", client.conn.RemoteAddr().String(), err)
			return
		}
		log.Printf("Sent welcome message to %s: %s", client.Nickname, strings.TrimSpace(msg))
	}

	sendMotd(client)
}

func removeClient(client *Client) {
	mu.Lock()
	defer mu.Unlock()
	for i, c := range clients {
		if c.conn == client.conn {
			clients = append(clients[:i], clients[i+1:]...)
			break
		}
	}
	for _, channel := range client.Channels {
		err := removeClientFromChannel(client, channel)
		if err != nil {
			log.Printf("Error removing client from channel: %v", err)
		}
	}
}

func sanitizeString(input string) string {
	var sb strings.Builder
	for _, r := range input {
		if r != ' ' && unicode.IsPrint(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func findClientByNickname(nickname string) *Client {
	mu.Lock()
	defer mu.Unlock()
	for _, client := range clients {
		if client.Nickname == nickname {
			return client
		}
	}
	return nil
}

func findChannel(name string) *Channel {
	mu.Lock()
	defer mu.Unlock()
	for _, channel := range channels {
		if channel.Name == name {
			return channel
		}
	}
	return nil
}

func getClientByNickname(nickname string) (*Client, error) {
	var client Client
	err := DB.Get(&client, "SELECT id, nickname, username, hostname, realname, password, invisible, is_operator, has_voice, created_at FROM users WHERE nickname = ?", nickname)
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func updateClientInfo(client *Client) error {
	_, err := DB.Exec(`
		UPDATE users 
		SET username = ?, hostname = ?, realname = ?, password = ?
		WHERE nickname = ?
	`, client.Username, client.Hostname, client.Realname, client.Password, client.Nickname)
	return err
}

func createClient(client *Client) error {
	result, err := DB.Exec(`
		INSERT INTO users (nickname, username, hostname, realname, password, created_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, client.Nickname, client.Username, client.Hostname, client.Realname, client.Password, client.CreatedAt, client.LastSeen)
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

// Add this new function to verify passwords
func verifyPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
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
