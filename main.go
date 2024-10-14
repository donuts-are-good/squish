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
)

const ServerNameString = "SquishIRC"
const ServerVersionString = "v0.1.2"

const (
	NickSuffix = "_"
)

// IRC numeric replies
const (
	RPL_WELCOME          = "001"
	RPL_YOURHOST         = "002"
	RPL_CREATED          = "003"
	RPL_MYINFO           = "004"
	RPL_ISUPPORT         = "005"
	RPL_UMODEIS          = "221"
	RPL_LUSERCLIENT      = "251"
	RPL_LUSEROP          = "252"
	RPL_LUSERUNKNOWN     = "253"
	RPL_LUSERCHANNELS    = "254"
	RPL_LUSERME          = "255"
	RPL_AWAY             = "301"
	RPL_UNAWAY           = "305"
	RPL_NOWAWAY          = "306"
	RPL_WHOISUSER        = "311"
	RPL_WHOISSERVER      = "312"
	RPL_WHOISOPERATOR    = "313"
	RPL_WHOISIDLE        = "317"
	RPL_ENDOFWHOIS       = "318"
	RPL_WHOISCHANNELS    = "319"
	RPL_CHANNELMODEIS    = "324"
	RPL_NOTOPIC          = "331"
	RPL_TOPIC            = "332"
	RPL_INVITING         = "341"
	RPL_NAMREPLY         = "353"
	RPL_ENDOFNAMES       = "366"
	RPL_MOTD             = "372"
	RPL_MOTDSTART        = "375"
	RPL_ENDOFMOTD        = "376"
	RPL_WHOREPLY         = "352"
	RPL_ENDOFWHO         = "315"
	RPL_YOUREOPER        = "381"
	ERR_UNKNOWNERROR     = "400"
	ERR_NOSUCHNICK       = "401"
	ERR_NOSUCHSERVER     = "402"
	ERR_NOSUCHCHANNEL    = "403"
	ERR_CANNOTSENDTOCHAN = "404"
	ERR_TOOMANYCHANNELS  = "405"
	ERR_WASNOSUCHNICK    = "406"
	ERR_NOORIGIN         = "409"
	ERR_NORECIPIENT      = "411"
	ERR_NOTEXTTOSEND     = "412"
	ERR_UNKNOWNCOMMAND   = "421"
	ERR_NOMOTD           = "422"
	ERR_NONICKNAMEGIVEN  = "431"
	ERR_ERRONEUSNICKNAME = "432"
	ERR_NICKNAMEINUSE    = "433"
	ERR_USERNOTINCHANNEL = "441"
	ERR_NOTONCHANNEL     = "442"
	ERR_USERONCHANNEL    = "443"
	ERR_NOTREGISTERED    = "451"
	ERR_NEEDMOREPARAMS   = "461"
	ERR_ALREADYREGISTRED = "462"
	ERR_PASSWDMISMATCH   = "464"
	ERR_CHANNELISFULL    = "471"
	ERR_UNKNOWNMODE      = "472"
	ERR_INVITEONLYCHAN   = "473"
	ERR_BANNEDFROMCHAN   = "474"
	ERR_BADCHANNELKEY    = "475"
	ERR_NOPRIVILEGES     = "481"
	ERR_CHANOPRIVSNEEDED = "482"
	ERR_CANTKILLSERVER   = "483"
	ERR_NOOPERHOST       = "491"
	ERR_UMODEUNKNOWNFLAG = "501"
	ERR_USERSDONTMATCH   = "502"
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
	DB               *sqlx.DB
	ChanServ         *ChanServType
	connectedClients map[string]*Client
	clientsMutex     sync.RWMutex
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

	// Initialize default channels
	initializeDefaultChannels()

	connectedClients = make(map[string]*Client)

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
	clientsMutex.RLock()
	defer clientsMutex.RUnlock()
	return connectedClients[nickname]
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

// Add this new function
func initializeDefaultChannels() {
	defaultChannels := []string{"#general", "#help", "#random"}
	for _, channelName := range defaultChannels {
		log.Printf("Initializing default channel: %s", channelName)
		channel, err := getOrCreateChannel(channelName)
		if err != nil {
			log.Printf("Error creating default channel %s: %v", channelName, err)
			continue
		}
		if !channel.IsRegistered {
			err = setChannelRegistered(channel.ID, 0) // Use 0 as the founder ID for server-created channels
			if err != nil {
				log.Printf("Error registering default channel %s: %v", channelName, err)
			} else {
				log.Printf("Default channel %s registered successfully", channelName)
			}
		} else {
			log.Printf("Default channel %s is already registered", channelName)
		}
	}
}

// Add these new functions to manage connected clients
func addConnectedClient(client *Client) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	connectedClients[client.Nickname] = client
}

func removeConnectedClient(nickname string) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	delete(connectedClients, nickname)
}

func updateConnectedClientNickname(oldNickname, newNickname string) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	if client, ok := connectedClients[oldNickname]; ok {
		delete(connectedClients, oldNickname)
		connectedClients[newNickname] = client
	}
}
