package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

const ServerNameString = "SquishIRC"
const ServerVersionString = "v0.1.1"

const (
	NickSuffix = "_"
)

var startTime = time.Now()

type Client struct {
	conn         net.Conn   `db:"-" json:"-"`
	ID           int64      `db:"id" json:"id"`
	Nickname     string     `db:"nickname" json:"nickname"`
	Username     string     `db:"username" json:"username"`
	Hostname     string     `db:"hostname" json:"hostname"`
	Realname     string     `db:"realname" json:"realname"`
	Password     string     `db:"password" json:"-"`
	Email        string     `db:"email" json:"email"`
	Channels     []*Channel `db:"-" json:"channels,omitempty"`
	Invisible    bool       `db:"invisible" json:"invisible"`
	IsOperator   bool       `db:"is_operator" json:"is_operator"`
	HasVoice     bool       `db:"has_voice" json:"has_voice"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	IsIdentified bool       `db:"is_identified" json:"is_identified"`
	LastSeen     time.Time  `db:"last_seen" json:"last_seen"`
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
	IsRegistered       bool           `db:"is_registered" json:"is_registered"`
	FounderID          sql.NullInt64  `db:"founder_id" json:"founder_id"`
}

// Add a new struct to represent the user_channels relationship
type UserChannel struct {
	UserID     int64     `db:"user_id"`
	ChannelID  int64     `db:"channel_id"`
	IsOperator bool      `db:"is_operator"`
	HasVoice   bool      `db:"has_voice"`
	JoinedAt   time.Time `db:"joined_at"`
}

type ChanServType struct {
	client *Client
}

var (
	DB       *sqlx.DB
	ChanServ *ChanServType
)

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

	// Initialize the ChanServ object
	ChanServ = NewChanServ()

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
	var client Client
	err := DB.Get(&client, "SELECT * FROM users WHERE nickname = ?", nickname)
	if err != nil {
		return nil
	}
	return &client
}

// Modify the handleNick function
func handleNick(client *Client, nickname string) {
	log.Printf("Handling NICK command for %s, new nickname: %s", client.conn.RemoteAddr().String(), nickname)
	if len(nickname) > 50 {
		log.Printf("Nickname too long: %s", nickname)
		client.conn.Write([]byte(fmt.Sprintf(":%s 432 * %s :Erroneous nickname\r\n", ServerNameString, nickname)))
		return
	}

	// Check if the nickname is already in use
	if isNicknameInUse(nickname) {
		// Nickname is in use
		if client.IsIdentified && client.Nickname == nickname {
			// Client is already identified for this nickname
			return
		}

		// Inform the client that the nickname is in use
		client.conn.Write([]byte(fmt.Sprintf(":%s 433 * %s :Nickname is already in use\r\n", ServerNameString, nickname)))

		// Generate a unique nickname
		uniqueNickname := generateUniqueNickname(nickname)
		client.conn.Write([]byte(fmt.Sprintf(":%s NOTICE * :Your nickname has been changed to %s\r\n", ServerNameString, uniqueNickname)))
		nickname = uniqueNickname
	}

	oldNickname := client.Nickname
	client.Nickname = nickname

	// Update the nickname in the database if the client is already registered
	if client.ID != 0 {
		err := updateClientNickname(client)
		if err != nil {
			log.Printf("Error updating client nickname in database: %v", err)
			client.Nickname = oldNickname
			client.conn.Write([]byte(fmt.Sprintf(":%s 432 %s :Nickname change failed\r\n", ServerNameString, nickname)))
			return
		}
	}

	// Notify the client and other users about the nickname change
	client.conn.Write([]byte(fmt.Sprintf(":%s NICK %s\r\n", oldNickname, nickname)))
	notifyNicknameChange(client, oldNickname, nickname)

	// Check if we have both NICK and USER info
	if client.Username != "" && client.ID == 0 {
		completeRegistration(client)
	}
}

// Add this helper function
func isNicknameInUse(nickname string) bool {
	_, err := getClientByNickname(nickname)
	return err == nil
}

func generateUniqueNickname(base string) string {
	newNickname := base
	for isNicknameInUse(newNickname) {
		randomSuffix := rand.Intn(8889) + 1111 // Generates a number between 1111 and 9999
		newNickname = fmt.Sprintf("%s-%s", base, strconv.Itoa(randomSuffix))
	}
	return newNickname
}

func notifyNicknameChange(client *Client, oldNickname, newNickname string) {
	for _, channel := range client.Channels {
		for _, c := range channel.Clients {
			if c != client {
				c.conn.Write([]byte(fmt.Sprintf(":%s NICK %s\r\n", oldNickname, newNickname)))
			}
		}
	}
}
