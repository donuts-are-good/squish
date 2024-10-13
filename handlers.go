package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func handleDisconnect(client *Client, err error) {
	var quitMessage string
	switch e := err.(type) {
	case net.Error:
		if e.Timeout() {
			quitMessage = "Ping timeout"
			log.Println("con: timeout:", client.conn.RemoteAddr().String())
		} else {
			quitMessage = "Connection error"
			log.Println("con: disconnect:", client.conn.RemoteAddr().String())
		}
	default:
		quitMessage = "Client quit"
		log.Println("con: disconnect:", client.conn.RemoteAddr().String())
	}

	handleQuit(client, quitMessage)
	removeClient(client)
}

func handlePrivmsg(client *Client, target string, message string) {
	if strings.EqualFold(target, "NickServ") {
		handleNickServMessage(client, message)
		return
	}

	if strings.HasPrefix(target, "#") {
		channel := findChannel(target)
		if channel != nil {
			broadcastMessage(channel, client, message)
		} else {
			client.conn.Write([]byte(fmt.Sprintf(":%s 403 %s %s :No such channel\r\n", ServerNameString, client.Nickname, target)))
		}
	} else {
		targetClient := findClientByNickname(target)
		if targetClient != nil {
			targetClient.conn.Write([]byte(fmt.Sprintf(":%s PRIVMSG %s :%s\r\n", client.Nickname, target, message)))
		} else {
			client.conn.Write([]byte(fmt.Sprintf(":%s 401 %s %s :No such nick/channel\r\n", ServerNameString, client.Nickname, target)))
		}
	}
}

func handleList(client *Client) {
	log.Println("handleList: start")
	channels, err := getAllChannels()
	if err != nil {
		log.Printf("Error getting channels: %v", err)
		client.conn.Write([]byte(fmt.Sprintf(":%s 323 %s :Error listing channels\r\n", ServerNameString, client.Nickname)))
		return
	}

	for _, channel := range channels {
		userCount, err := getChannelUserCount(channel.ID)
		if err != nil {
			log.Printf("Error getting user count for channel %s: %v", channel.Name, err)
			continue
		}
		client.conn.Write([]byte(fmt.Sprintf(":%s 322 %s %s %d :%s\r\n", ServerNameString, client.Nickname, channel.Name, userCount, channel.Topic)))
	}
	client.conn.Write([]byte(fmt.Sprintf(":%s 323 %s :End of /LIST\r\n", ServerNameString, client.Nickname)))
}

func handleNames(client *Client, channelName string) {
	log.Println("handleUsers: starting")
	var users []string

	mu.Lock()
	defer mu.Unlock()

	if channelName == "" {
		for _, c := range clients {
			users = append(users, c.Nickname)
		}
	} else {
		channel := findChannel(channelName)
		if channel != nil {
			for _, c := range channel.Clients {
				users = append(users, c.Nickname)
			}
		} else {
			client.conn.Write([]byte(fmt.Sprintf(":%s 403 %s %s No such channel\r\n", ServerNameString, client.Nickname, channelName)))
			return
		}
	}

	userList := strings.Join(users, " ")

	if channelName == "" {
		client.conn.Write([]byte(fmt.Sprintf(":%s 265 %s %s\r\n", ServerNameString, client.Nickname, userList)))
	} else {
		client.conn.Write([]byte(fmt.Sprintf(":%s 353 %s = %s :%s\r\n", ServerNameString, client.Nickname, channelName, userList)))
		client.conn.Write([]byte(fmt.Sprintf(":%s 366 %s %s End of /NAMES list.\r\n", ServerNameString, client.Nickname, channelName)))
	}
}

func handleJoin(client *Client, channelName string) {
	if !strings.HasPrefix(channelName, "#") {
		channelName = "#" + channelName
	}

	channel, err := getOrCreateChannel(channelName)
	if err != nil {
		log.Printf("Error getting or creating channel: %v", err)
		client.conn.Write([]byte(fmt.Sprintf(":%s 403 %s %s :Failed to join channel\r\n", ServerNameString, client.Nickname, channelName)))
		return
	}

	err = addClientToChannel(client, channel)
	if err != nil {
		log.Printf("Error adding client to channel: %v", err)
		client.conn.Write([]byte(fmt.Sprintf(":%s 403 %s %s :Failed to join channel\r\n", ServerNameString, client.Nickname, channelName)))
		return
	}

	clients, err := getClientsInChannel(channel)
	if err != nil {
		log.Printf("Error getting clients in channel: %v", err)
	} else {
		channel.Clients = clients
	}

	mu.Lock()
	channel.Clients = append(channel.Clients, client)
	client.Channels = append(client.Channels, channel)
	mu.Unlock()

	for _, c := range channel.Clients {
		c.conn.Write([]byte(fmt.Sprintf(":%s JOIN %s\r\n", client.Nickname, channel.Name)))
	}

	// Send channel topic
	if channel.Topic != "" {
		client.conn.Write([]byte(fmt.Sprintf(":%s 332 %s %s :%s\r\n", ServerNameString, client.Nickname, channel.Name, channel.Topic)))
	} else {
		client.conn.Write([]byte(fmt.Sprintf(":%s 331 %s %s :No topic is set\r\n", ServerNameString, client.Nickname, channel.Name)))
	}

	// Send names list
	var nicknames []string
	for _, c := range channel.Clients {
		nicknames = append(nicknames, c.Nickname)
	}
	nicknameList := strings.Join(nicknames, " ")
	client.conn.Write([]byte(fmt.Sprintf(":%s 353 %s = %s :%s\r\n", ServerNameString, client.Nickname, channel.Name, nicknameList)))
	client.conn.Write([]byte(fmt.Sprintf(":%s 366 %s %s :End of /NAMES list.\r\n", ServerNameString, client.Nickname, channel.Name)))
}

func handleCap(client *Client, params string) {
	log.Printf("Handling CAP command: %s", params)
	parts := strings.SplitN(params, " ", 2)
	if len(parts) > 1 {
		subCommand := parts[0]
		switch subCommand {
		case "LS":
			// Check if the client requested a specific CAP version
			if len(parts) > 1 && strings.HasPrefix(parts[1], "302") {
				client.conn.Write([]byte("CAP * LS * :multi-prefix\r\n"))
			} else {
				client.conn.Write([]byte("CAP * LS :multi-prefix\r\n"))
			}
			log.Printf("Sent CAP LS response to %s", client.conn.RemoteAddr().String())
		case "REQ":
			// Respond to capability requests. We support multi-prefix.
			requestedCaps := strings.Split(parts[1], " ")
			unsupportedCaps := make([]string, 0)
			for _, cap := range requestedCaps {
				if cap != "multi-prefix" {
					unsupportedCaps = append(unsupportedCaps, cap)
				}
			}
			if len(unsupportedCaps) > 0 {
				client.conn.Write([]byte(fmt.Sprintf("CAP * NAK :%s\r\n", strings.Join(unsupportedCaps, " "))))
			} else {
				client.conn.Write([]byte(fmt.Sprintf("CAP * ACK :%s\r\n", parts[1])))
			}
		case "END":
			// End of CAP negotiation.
			client.conn.Write([]byte("CAP * END\r\n"))
		case "LIST":
			// List the capabilities currently enabled for the client. Since we support multi-prefix, respond with it.
			client.conn.Write([]byte("CAP * LIST :multi-prefix\r\n"))
		case "CLEAR":
			// Clear all capabilities. Since we support multi-prefix, just acknowledge the command.
			client.conn.Write([]byte("CAP * CLEAR :\r\n"))
		default:
			// Unknown subcommand, respond with an error.
			client.conn.Write([]byte(fmt.Sprintf(":%s 410 %s :Invalid CAP subcommand\r\n", ServerNameString, client.Nickname)))
		}
	} else {
		log.Printf("Invalid CAP command from %s: %s", client.conn.RemoteAddr().String(), params)
		client.conn.Write([]byte(fmt.Sprintf(":%s 410 %s :Invalid CAP command\r\n", ServerNameString, client.Nickname)))
	}
}

func handlePart(client *Client, channelName string) {
	channel := findChannel(channelName)
	if channel == nil {
		client.conn.Write([]byte(fmt.Sprintf(":%s 403 %s %s :No such channel\r\n", ServerNameString, client.Nickname, channelName)))
		return
	}

	err := removeClientFromChannel(client, channel)
	if err != nil {
		log.Printf("Error removing client from channel: %v", err)
	}

	client.conn.Write([]byte(fmt.Sprintf(":%s!%s@%s PART %s\r\n", client.Nickname, client.Nickname, client.conn.RemoteAddr().String(), channelName)))

	// Notify other users in the channel
	for _, c := range channel.Clients {
		c.conn.Write([]byte(fmt.Sprintf(":%s!%s@%s PART %s\r\n", client.Nickname, client.Nickname, client.conn.RemoteAddr().String(), channelName)))
	}
}

func handleQuit(client *Client, message string) {
	quitMessage := fmt.Sprintf(":%s!%s@%s QUIT :%s\r\n", client.Nickname, client.Nickname, client.conn.RemoteAddr().String(), message)

	// Notify all channels the user was in
	for _, channel := range client.Channels {
		for _, c := range channel.Clients {
			if c != client {
				c.conn.Write([]byte(quitMessage))
			}
		}
	}

	removeClient(client)
	client.conn.Close()
}

func handleUser(client *Client, username, hostname, realname string) {
	log.Printf("Handling USER command for %s: username=%s, hostname=%s, realname=%s", client.conn.RemoteAddr().String(), username, hostname, realname)

	client.Username = username
	client.Hostname = hostname
	client.Realname = realname

	// Check if we have both NICK and USER info
	if client.Nickname != "" {
		completeRegistration(client)
	} else {
		// Send a message to guide the user
		client.conn.Write([]byte(fmt.Sprintf(":%s NOTICE * :Welcome! Please set your nickname using the NICK command.\r\n", ServerNameString)))
	}
}

func completeRegistration(client *Client) {
	// Generate a random password
	password := generateRandomPassword(10)

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		client.conn.Write([]byte(fmt.Sprintf(":%s 451 %s :Failed to register (password hashing error)\r\n", ServerNameString, client.Nickname)))
		return
	}

	err = createOrUpdateClient(client, string(hashedPassword))
	if err != nil {
		log.Printf("Error updating client information: %v", err)
		client.conn.Write([]byte(fmt.Sprintf(":%s 451 %s :Failed to register (database error)\r\n", ServerNameString, client.Nickname)))
		return
	}

	log.Printf("User registration complete for %s", client.Nickname)
	sendWelcomeMessages(client)

	// Send the password and instructions to the user
	client.conn.Write([]byte(fmt.Sprintf(":%s NOTICE %s :Your account username: '%s' nickname: '%s' has been registered with the password: %s\r\n", ServerNameString, client.Nickname, client.Username, client.Nickname, password)))
	client.conn.Write([]byte(fmt.Sprintf(":%s NOTICE %s :To change your password, use the command: /msg NickServ SET PASSWORD <new_password>\r\n", ServerNameString, client.Nickname)))
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
		client.Password = password // Add this line
		err = updateClientInfo(client)
	} else {
		return err
	}
	return err
}

func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	password := make([]byte, length)
	for i := range password {
		password[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(password)
}

func handleMode(client *Client, target string, modes string) {
	if strings.HasPrefix(target, "#") {
		handleChannelMode(client, target, modes)
	} else {
		handleUserMode(client, target, modes)
	}
}

func handleUserMode(client *Client, target string, modes string) {
	if target != client.Nickname {
		client.conn.Write([]byte(fmt.Sprintf(":%s 502 %s :Can't change mode for other users\r\n", ServerNameString, client.Nickname)))
		return
	}

	adding := true
	for _, mode := range modes {
		switch mode {
		case '+':
			adding = true
		case '-':
			adding = false
		case 'i':
			client.Invisible = adding
			_, err := DB.Exec("UPDATE users SET invisible = ? WHERE nickname = ?", adding, client.Nickname)
			if err != nil {
				log.Printf("Error updating user invisible mode: %v", err)
			}
		case 'o':
			// 'o' mode should only be set by the OPER command, not by MODE
			if adding {
				client.conn.Write([]byte(fmt.Sprintf(":%s 481 %s :Permission Denied- You're not an IRC operator\r\n", ServerNameString, client.Nickname)))
			}
		}
	}

	// Notify the user of their new modes
	client.conn.Write([]byte(fmt.Sprintf(":%s MODE %s :%s\r\n", client.Nickname, client.Nickname, modes)))
}

func handleChannelMode(client *Client, channelName string, modes string) {
	channel := findChannel(channelName)
	if channel == nil {
		client.conn.Write([]byte(fmt.Sprintf(":%s 403 %s %s :No such channel\r\n", ServerNameString, client.Nickname, channelName)))
		return
	}

	// Check if the client is a channel operator
	var isOperator bool
	err := DB.QueryRow("SELECT is_operator FROM user_channels WHERE user_id = ? AND channel_id = ?", client.ID, channel.ID).Scan(&isOperator)
	if err != nil {
		if err == sql.ErrNoRows {
			client.conn.Write([]byte(fmt.Sprintf(":%s 442 %s %s :You're not on that channel\r\n", ServerNameString, client.Nickname, channelName)))
		} else {
			log.Printf("Error checking operator status: %v", err)
			client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Internal server error\r\n", ServerNameString, client.Nickname)))
		}
		return
	}

	// If no modes are provided, list the current channel modes
	if modes == "" {
		listChannelModes(client, channel)
		return
	}

	if !isOperator {
		client.conn.Write([]byte(fmt.Sprintf(":%s 482 %s %s :You're not channel operator\r\n", ServerNameString, client.Nickname, channelName)))
		return
	}

	modeArgs := strings.Fields(modes)
	modeString := modeArgs[0]
	modeArgs = modeArgs[1:]
	adding := true
	argIndex := 0

	for _, mode := range modeString {
		switch mode {
		case '+':
			adding = true
		case '-':
			adding = false
		case 'n':
			channel.NoExternalMessages = adding
			_, err := DB.Exec("UPDATE channels SET no_external_messages = ? WHERE name = ?", adding, channelName)
			if err != nil {
				log.Printf("Error updating channel no_external_messages mode: %v", err)
				client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error setting mode 'n'\r\n", ServerNameString, client.Nickname)))
			}
		case 't':
			channel.TopicProtection = adding
			_, err := DB.Exec("UPDATE channels SET topic_protection = ? WHERE name = ?", adding, channelName)
			if err != nil {
				log.Printf("Error updating channel topic_protection mode: %v", err)
				client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error setting mode 't'\r\n", ServerNameString, client.Nickname)))
			}
		case 'm':
			channel.Moderated = adding
			_, err := DB.Exec("UPDATE channels SET moderated = ? WHERE name = ?", adding, channelName)
			if err != nil {
				log.Printf("Error updating channel moderated mode: %v", err)
				client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error setting mode 'm'\r\n", ServerNameString, client.Nickname)))
			}
		case 'i':
			channel.InviteOnly = adding
			_, err := DB.Exec("UPDATE channels SET invite_only = ? WHERE name = ?", adding, channelName)
			if err != nil {
				log.Printf("Error updating channel invite_only mode: %v", err)
				client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error setting mode 'i'\r\n", ServerNameString, client.Nickname)))
			}
		case 'k':
			if adding && argIndex < len(modeArgs) {
				channel.Key = sql.NullString{String: modeArgs[argIndex], Valid: true}
				_, err := DB.Exec("UPDATE channels SET key = ? WHERE name = ?", channel.Key.String, channelName)
				if err != nil {
					log.Printf("Error updating channel key: %v", err)
					client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error setting mode 'k'\r\n", ServerNameString, client.Nickname)))
				}
				argIndex++
			} else if !adding {
				channel.Key = sql.NullString{String: "", Valid: false}
				_, err := DB.Exec("UPDATE channels SET key = NULL WHERE name = ?", channelName)
				if err != nil {
					log.Printf("Error removing channel key: %v", err)
					client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error removing mode 'k'\r\n", ServerNameString, client.Nickname)))
				}
			} else {
				client.conn.Write([]byte(fmt.Sprintf(":%s 461 %s MODE :Not enough parameters\r\n", ServerNameString, client.Nickname)))
			}
		case 'l':
			if adding && argIndex < len(modeArgs) {
				limit, err := strconv.Atoi(modeArgs[argIndex])
				if err == nil {
					channel.UserLimit = limit
					_, err := DB.Exec("UPDATE channels SET user_limit = ? WHERE name = ?", limit, channelName)
					if err != nil {
						log.Printf("Error updating channel user limit: %v", err)
						client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error setting mode 'l'\r\n", ServerNameString, client.Nickname)))
					}
				} else {
					client.conn.Write([]byte(fmt.Sprintf(":%s 461 %s MODE :Invalid user limit\r\n", ServerNameString, client.Nickname)))
				}
				argIndex++
			} else if !adding {
				channel.UserLimit = 0
				_, err := DB.Exec("UPDATE channels SET user_limit = 0 WHERE name = ?", channelName)
				if err != nil {
					log.Printf("Error removing channel user limit: %v", err)
					client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error removing mode 'l'\r\n", ServerNameString, client.Nickname)))
				}
			} else {
				client.conn.Write([]byte(fmt.Sprintf(":%s 461 %s MODE :Not enough parameters\r\n", ServerNameString, client.Nickname)))
			}
		case 'o', 'v':
			if argIndex < len(modeArgs) {
				targetNick := modeArgs[argIndex]
				var targetClientID int
				err := DB.QueryRow("SELECT id FROM users WHERE nickname = ?", targetNick).Scan(&targetClientID)
				if err != nil {
					if err == sql.ErrNoRows {
						client.conn.Write([]byte(fmt.Sprintf(":%s 401 %s %s :No such nick\r\n", ServerNameString, client.Nickname, targetNick)))
					} else {
						log.Printf("Error fetching user ID: %v", err)
						client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Internal server error\r\n", ServerNameString, client.Nickname)))
					}
				} else {
					if mode == 'o' {
						_, err := DB.Exec("UPDATE user_channels SET is_operator = ? WHERE user_id = ? AND channel_id = ?", adding, targetClientID, channel.ID)
						if err != nil {
							log.Printf("Error updating user channel operator status: %v", err)
							client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error setting mode 'o'\r\n", ServerNameString, client.Nickname)))
						}
					} else { // 'v'
						_, err := DB.Exec("UPDATE user_channels SET has_voice = ? WHERE user_id = ? AND channel_id = ?", adding, targetClientID, channel.ID)
						if err != nil {
							log.Printf("Error updating user channel voice status: %v", err)
							client.conn.Write([]byte(fmt.Sprintf(":%s 500 %s :Error setting mode 'v'\r\n", ServerNameString, client.Nickname)))
						}
					}
				}
				argIndex++
			} else {
				client.conn.Write([]byte(fmt.Sprintf(":%s 461 %s MODE :Not enough parameters\r\n", ServerNameString, client.Nickname)))
			}
		default:
			client.conn.Write([]byte(fmt.Sprintf(":%s 472 %s %c :is unknown mode char to me\r\n", ServerNameString, client.Nickname, mode)))
		}
	}

	// Notify all users in the channel about the mode change
	notifyChannelModeChange(client, channel, modeString, modeArgs)
}

func listChannelModes(client *Client, channel *Channel) {
	var modeString string
	var modeArgs []string

	if channel.NoExternalMessages {
		modeString += "n"
	}
	if channel.TopicProtection {
		modeString += "t"
	}
	if channel.Moderated {
		modeString += "m"
	}
	if channel.InviteOnly {
		modeString += "i"
	}
	if channel.Key.Valid && channel.Key.String != "" {
		modeString += "k"
		modeArgs = append(modeArgs, channel.Key.String)
	}
	if channel.UserLimit > 0 {
		modeString += "l"
		modeArgs = append(modeArgs, strconv.Itoa(channel.UserLimit))
	}

	if modeString != "" {
		modeString = "+" + modeString
	}

	client.conn.Write([]byte(fmt.Sprintf(":%s 324 %s %s %s %s\r\n", ServerNameString, client.Nickname, channel.Name, modeString, strings.Join(modeArgs, " "))))
}

func notifyChannelModeChange(client *Client, channel *Channel, modeString string, modeArgs []string) {
	rows, err := DB.Query("SELECT u.nickname FROM users u JOIN user_channels uc ON u.id = uc.user_id WHERE uc.channel_id = ?", channel.ID)
	if err != nil {
		log.Printf("Error fetching channel users: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var nickname string
		if err := rows.Scan(&nickname); err != nil {
			log.Printf("Error scanning user nickname: %v", err)
			continue
		}
		targetClient := findClientByNickname(nickname)
		if targetClient != nil {
			targetClient.conn.Write([]byte(fmt.Sprintf(":%s MODE %s %s %s\r\n", client.Nickname, channel.Name, modeString, strings.Join(modeArgs, " "))))
		}
	}
}

func handleTopic(client *Client, channelName string, newTopic string) {
	channel := findChannel(channelName)
	if channel == nil {
		client.conn.Write([]byte(fmt.Sprintf(":%s 403 %s %s :No such channel\r\n", ServerNameString, client.Nickname, channelName)))
		return
	}

	if newTopic == "" {
		// Send current topic
		client.conn.Write([]byte(fmt.Sprintf(":%s 332 %s %s :%s\r\n", ServerNameString, client.Nickname, channelName, channel.Topic)))
	} else {
		// Set new topic
		channel.Topic = newTopic
		for _, c := range channel.Clients {
			c.conn.Write([]byte(fmt.Sprintf(":%s!%s@%s TOPIC %s :%s\r\n", client.Nickname, client.Nickname, client.conn.RemoteAddr().String(), channelName, newTopic)))
		}
	}
}

func handleWho(client *Client, target string) {
	log.Printf("Handling WHO command for target: %s", target)
	// For now, just send an empty WHO reply
	client.conn.Write([]byte(fmt.Sprintf(":%s 315 %s %s :End of WHO list\r\n", ServerNameString, client.Nickname, target)))
}

// Add these new functions to handlers.go

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
