package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"time"
)

func handleDisconnect(client *Client, err error) {
	switch e := err.(type) {
	case net.Error:
		if e.Timeout() {
			log.Println("con: timeout:", client.conn.RemoteAddr().String())
		} else {
			log.Println("con: disconnect:", client.conn.RemoteAddr().String())
		}
	default:
		log.Println("con: disconnect:", client.conn.RemoteAddr().String())
	}

	removeClient(client)
}

func handlePrivmsg(client *Client, target string, message string) {
	if strings.HasPrefix(target, "#") {
		channel := findChannel(target)
		if channel != nil {
			broadcastMessage(channel, client, message)
		}
	} else {
		targetClient := findClientByNickname(target)
		if targetClient != nil {
			targetClient.conn.Write([]byte(fmt.Sprintf(":%s PRIVMSG %s %s\r\n", client.nickname, target, message)))
		} else {
			client.conn.Write([]byte(fmt.Sprintf(":%s 401 %s No such nick/channel\r\n", ServerNameString, target)))
		}
	}
}

func handleList(client *Client) {
	log.Println("handleList: start")
	mu.Lock()
	defer mu.Unlock()
	for _, channel := range channels {
		userCount := len(channel.clients)
		client.conn.Write([]byte(fmt.Sprintf(":%s 322 %s %s %d %s\r\n", ServerNameString, client.nickname, channel.name, userCount, channel.topic)))
	}
}

func handleNames(client *Client, channelName string) {
	log.Println("handleUsers: starting")
	var users []string

	mu.Lock()
	defer mu.Unlock()

	if channelName == "" {
		for _, c := range clients {
			users = append(users, c.nickname)
		}
	} else {
		channel := findChannel(channelName)
		if channel != nil {
			for _, c := range channel.clients {
				users = append(users, c.nickname)
			}
		} else {
			client.conn.Write([]byte(fmt.Sprintf(":%s 403 %s %s No such channel\r\n", ServerNameString, client.nickname, channelName)))
			return
		}
	}

	userList := strings.Join(users, " ")

	if channelName == "" {
		client.conn.Write([]byte(fmt.Sprintf(":%s 265 %s %s\r\n", ServerNameString, client.nickname, userList)))
	} else {
		client.conn.Write([]byte(fmt.Sprintf(":%s 353 %s = %s :%s\r\n", ServerNameString, client.nickname, channelName, userList)))
		client.conn.Write([]byte(fmt.Sprintf(":%s 366 %s %s End of /NAMES list.\r\n", ServerNameString, client.nickname, channelName)))
	}
}

func handleNick(client *Client, nickname string, conn net.Conn) {
	if len(nickname) > 16 {
		client.conn.Write([]byte("ERROR: Nickname too long.\n"))
		return
	}

	maxAttempts := 100
	attempts := 0

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for attempts < maxAttempts {
		randomNumber := r.Intn(9000) + 1000
		uniqueNickname := fmt.Sprintf("%s(#%04d)", nickname, randomNumber)

		isUnique := true
		mu.Lock()
		for _, c := range clients {
			if c.nickname == uniqueNickname {
				isUnique = false
				break
			}
		}
		mu.Unlock()

		if isUnique {
			client.nickname = uniqueNickname

			mu.Lock()
			for i, c := range clients {
				if c.conn == client.conn {
					clients = append(clients[:i], clients[i+1:]...)
					break
				}
			}
			clients = append(clients, client)
			mu.Unlock()

			client.conn.Write([]byte(fmt.Sprintf("NICK %s\n", uniqueNickname)))
			return
		}

		attempts++
	}

	client.conn.Write([]byte("ERROR: Unable to assign a unique nickname.\n"))
	conn.Close()
}

func handleJoin(client *Client, channelName string) {
	if !strings.HasPrefix(channelName, "#") {
		channelName = "#" + channelName
	}

	mu.Lock()
	channel := findChannel(channelName)
	if channel == nil {
		channel = &Channel{name: channelName, topic: "Welcome to " + channelName}
		channels = append(channels, channel)
	}
	channel.clients = append(channel.clients, client)
	client.channels = append(client.channels, channel)
	mu.Unlock()

	for _, c := range channel.clients {
		if c != client {
			c.conn.Write([]byte(fmt.Sprintf(":%s JOIN %s\r\n", client.nickname, channel.name)))
		}
	}

	var nicknames []string
	for _, c := range channel.clients {
		nicknames = append(nicknames, c.nickname)
	}
	nicknameList := strings.Join(nicknames, " ")
	client.conn.Write([]byte(fmt.Sprintf(":%s 353 %s = %s :%s\r\n", ServerNameString, client.nickname, channel.name, nicknameList)))
	client.conn.Write([]byte(fmt.Sprintf(":%s 366 %s %s End of /NAMES list.\r\n", ServerNameString, client.nickname, channel.name)))
	client.conn.Write([]byte(fmt.Sprintf(":%s 331 %s %s No topic is set\r\n", ServerNameString, client.nickname, channel.name)))
}

func handleCap(client *Client, params string) {
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
			client.conn.Write([]byte(fmt.Sprintf(":%s 410 %s :Invalid CAP subcommand\r\n", ServerNameString, client.nickname)))
		}
	} else {
		// If no subcommand is provided, respond with an error.
		client.conn.Write([]byte(fmt.Sprintf(":%s 410 %s :Invalid CAP command\r\n", ServerNameString, client.nickname)))
	}
}
