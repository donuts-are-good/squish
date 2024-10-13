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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic in handleConnection: %v", r)
		}
		log.Printf("Connection closed for %s", conn.RemoteAddr().String())
		conn.Close()
	}()

	client := &Client{conn: conn, Nickname: ""}
	reader := bufio.NewReader(conn)

	mu.Lock()
	clients = append(clients, client)
	mu.Unlock()

	log.Printf("New connection from %s", conn.RemoteAddr().String())

	// Send a preliminary welcome message
	_, err := conn.Write([]byte(fmt.Sprintf(":%s NOTICE Auth :*** Looking up your hostname...\r\n", ServerNameString)))
	if err != nil {
		log.Printf("Error sending initial message: %v", err)
		return
	}

	pingTicker := time.NewTicker(1 * time.Minute)
	defer pingTicker.Stop()

	lastPingResponse := time.Now()

	log.Printf("Starting main loop for %s", conn.RemoteAddr().String())
	for {
		select {
		case <-pingTicker.C:
			if time.Since(lastPingResponse) > 2*time.Minute {
				log.Printf("Ping timeout for %s", conn.RemoteAddr().String())
				handleDisconnect(client, fmt.Errorf("ping timeout"))
				return
			}
			log.Printf("Sending PING to %s", conn.RemoteAddr().String())
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

			lastPingResponse = time.Now()

			log.Printf("Received message from %s: %s", conn.RemoteAddr().String(), strings.TrimSpace(message))
			message = strings.Trim(message, "\r\n")
			parts := strings.SplitN(message, " ", 2)

			if len(parts) > 1 {
				command, params := parts[0], parts[1]
				if commandParser(client, command, params) {
					log.Printf("Client %s requested disconnect", conn.RemoteAddr().String())
					return
				}
			} else if len(parts) == 1 {
				command := parts[0]
				if commandParser(client, command, "") {
					log.Printf("Client %s requested disconnect", conn.RemoteAddr().String())
					return
				}
			}
		}
	}
}

func commandParser(client *Client, command, params string) bool {
	command = strings.ToUpper(command) // Convert command to uppercase
	switch command {
	case "PING":
		client.conn.Write([]byte(fmt.Sprintf("PONG %s\r\n", params)))
	case "PONG":
		log.Println("PONG!")
	case "NICK":
		log.Println("command: nick")
		sanitizedNickname := sanitizeString(params)
		handleNick(client, sanitizedNickname)
	case "USER":
		log.Println("command: user")
		userParts := strings.SplitN(params, " ", 4)
		if len(userParts) == 4 {
			handleUser(client, userParts[0], userParts[1], userParts[2])
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
		channelName := strings.Split(params, " ")[0] // Take only the first parameter
		sanitizedChannelName := sanitizeString(channelName)
		handleJoin(client, sanitizedChannelName)
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
			handlePrivmsg(client, target, message)
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
	default:
		log.Printf("Unhandled command: %s\n", command)
		client.conn.Write([]byte(fmt.Sprintf(":%s 421 %s %s :Unknown command\r\n", ServerNameString, client.Nickname, command)))
	}
	return false
}
