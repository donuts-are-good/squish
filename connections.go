package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

func handleConnection(conn net.Conn) {
	client := &Client{conn: conn, Nickname: ""}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic in handleConnection: %v", r)
		}
		log.Printf("Connection closed for %s", conn.RemoteAddr().String())
		removeConnectedClient(client.Nickname)
		conn.Close()
	}()

	reader := bufio.NewReader(conn)

	log.Printf("New connection from %s", conn.RemoteAddr().String())

	// Send a preliminary welcome message
	_, err := conn.Write([]byte(fmt.Sprintf(":%s NOTICE Auth :*** Looking up your hostname...\r\n", ServerNameString)))
	if err != nil {
		log.Printf("Error sending initial message: %v", err)
		return
	}

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	lastPingResponse := time.Now()
	var lastPingSent time.Time

	log.Printf("Starting main loop for %s", conn.RemoteAddr().String())
	for {
		select {
		case <-pingTicker.C:
			if time.Since(lastPingResponse) > 60*time.Second && !lastPingSent.IsZero() {
				log.Printf("Ping timeout for %s", conn.RemoteAddr().String())
				handleDisconnect(client, fmt.Errorf("ping timeout"))
				return
			}
			log.Printf("Sending PING to %s", conn.RemoteAddr().String())
			lastPingSent = time.Now()
			_, err := conn.Write([]byte(fmt.Sprintf("PING :%s\r\n", ServerNameString)))
			if err != nil {
				log.Printf("Error sending PING: %v", err)
				handleDisconnect(client, err)
				return
			}

		default:
			conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
			message, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("Error reading from connection %s: %v", conn.RemoteAddr().String(), err)
				handleDisconnect(client, err)
				return
			}

			log.Printf("Received message from %s: %s", conn.RemoteAddr().String(), strings.TrimSpace(message))
			message = strings.Trim(message, "\r\n")
			parts := strings.SplitN(message, " ", 2)

			if len(parts) > 0 {
				command := strings.ToUpper(parts[0])
				params := ""
				if len(parts) > 1 {
					params = parts[1]
				}
				if command == "PONG" {
					lastPingResponse = time.Now()
					lastPingSent = time.Time{}
					continue
				}

				if commandParser(client, command, params) {
					log.Printf("Client %s requested disconnect", conn.RemoteAddr().String())
					return
				}
			}
		}
	}
}

func commandParser(client *Client, command, params string) bool {
	switch command {
	case "PING":
		client.conn.Write([]byte(fmt.Sprintf("PONG %s\r\n", params)))
	case "NICK":
		log.Println("command: nick")
		sanitizedNickname := sanitizeString(params)
		handleNick(client, sanitizedNickname)
	case "USER":
		log.Println("command: user")
		userParts := strings.SplitN(params, " ", 4)
		if len(userParts) == 4 {
			handleUser(client, userParts[0], userParts[1], userParts[3])
		}
	case "NAMES":
		log.Println("command: names")
		targetAndChannel := strings.SplitN(params, " ", 2)
		var channelName string
		if len(targetAndChannel) > 1 {
			channelName = sanitizeString(targetAndChannel[1])
		}
		handleNames(client, channelName)
	case "JOIN":
		log.Println("command: join")
		channelNames := strings.TrimSpace(params)
		handleJoin(client, channelNames)
	case "PART":
		log.Println("command: part")
		sanitizedChannelName := sanitizeString(params)
		handlePart(client, sanitizedChannelName)
	case "QUIT":
		log.Println("command: quit")
		handleQuit(client, params)
		return true
	case "LIST":
		log.Println("command: list")
		handleList(client)
	case "PRIVMSG":
		log.Println("command: privmsg")
		targetAndMessage := strings.SplitN(params, " ", 2)
		if len(targetAndMessage) > 1 {
			target, message := targetAndMessage[0], targetAndMessage[1]
			if strings.EqualFold(target, "NickServ") {
				log.Printf("NickServ command received from %s: %s", client.Nickname, message)
				handleNickServMessage(client, strings.TrimPrefix(message, ":"))
				// Send an acknowledgment to the client
				client.conn.Write([]byte(fmt.Sprintf(":%s NOTICE %s :NickServ command processed\r\n", ServerNameString, client.Nickname)))
			} else if strings.EqualFold(target, "ChanServ") {
				log.Printf("ChanServ command received from %s: %s", client.Nickname, message)
				ChanServ.HandleMessage(client, strings.TrimPrefix(message, ":"))
				// Send an acknowledgment to the client
				client.conn.Write([]byte(fmt.Sprintf(":%s NOTICE %s :ChanServ command processed\r\n", ServerNameString, client.Nickname)))
			} else {
				handlePrivmsg(client, target, message)
			}
		} else {
			log.Printf("Invalid PRIVMSG format from %s: %s", client.Nickname, params)
			client.conn.Write([]byte(fmt.Sprintf(":%s NOTICE %s :Invalid PRIVMSG format\r\n", ServerNameString, client.Nickname)))
		}
	case "MODE":
		log.Println("command: mode")
		modeParts := strings.SplitN(params, " ", 2)
		if len(modeParts) > 1 {
			handleMode(client, modeParts[0], modeParts[1])
		}
	case "TOPIC":
		log.Println("command: topic")
		topicParts := strings.SplitN(params, " ", 2)
		if len(topicParts) > 1 {
			handleTopic(client, topicParts[0], topicParts[1])
		} else {
			handleTopic(client, topicParts[0], "")
		}
	case "CAP":
		handleCap(client, params)
	case "MOTD":
		sendMotd(client)
	case "WHO":
		log.Println("command: who")
		targetParts := strings.SplitN(params, " ", 2)
		handleWho(client, targetParts[0])
	case "WHOIS":
		log.Println("command: whois")
		targetParts := strings.SplitN(params, " ", 2)
		handleWhois(client, targetParts[0])
	default:
		log.Printf("Unhandled command: %s\n", command)
		client.conn.Write([]byte(fmt.Sprintf(":%s 421 %s %s :Unknown command\r\n", ServerNameString, client.Nickname, command)))
	}
	return false
}

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
	// Remove client from all channels in the database
	_, err = DB.Exec("DELETE FROM user_channels WHERE user_id = ?", client.ID)
	if err != nil {
		log.Printf("Error removing client from channels: %v", err)
	}
	// Update last_seen in the database
	_, err = DB.Exec("UPDATE users SET last_seen = ? WHERE id = ?", time.Now(), client.ID)
	if err != nil {
		log.Printf("Error updating last_seen for client: %v", err)
	}
	// Remove the client from the active sessions
	removeConnectedClient(client.Nickname)
	// Reset the client's identification status
	client.IsIdentified = false
	_, err = DB.Exec("UPDATE users SET is_identified = ? WHERE id = ?", false, client.ID)
	if err != nil {
		log.Printf("Error resetting identification status for client: %v", err)
	}
	log.Printf("Client %s (%s) has been disconnected and removed from active sessions", client.Nickname, client.conn.RemoteAddr().String())
}
