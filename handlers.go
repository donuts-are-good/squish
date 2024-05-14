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
			client.conn.Write([]byte(fmt.Sprintf(":%s 401 %s No such nick/channel\r\n", "serverName", target)))
		}
	}
}

func handleList(client *Client) {
	log.Println("handleList: start")
	mu.Lock()
	defer mu.Unlock()
	for _, channel := range channels {
		userCount := len(channel.clients)
		client.conn.Write([]byte(fmt.Sprintf(":%s 322 %s %s %d %s\r\n", "serverName", client.nickname, channel.name, userCount, channel.topic)))
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
			client.conn.Write([]byte(fmt.Sprintf(":%s 403 %s %s No such channel\r\n", "serverName", client.nickname, channelName)))
			return
		}
	}

	userList := strings.Join(users, " ")

	if channelName == "" {
		client.conn.Write([]byte(fmt.Sprintf(":%s 265 %s %s\r\n", "serverName", client.nickname, userList)))
	} else {
		client.conn.Write([]byte(fmt.Sprintf(":%s 353 %s = %s :%s\r\n", "serverName", client.nickname, channelName, userList)))
		client.conn.Write([]byte(fmt.Sprintf(":%s 366 %s %s End of /NAMES list.\r\n", "serverName", client.nickname, channelName)))
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
	client.conn.Write([]byte(fmt.Sprintf(":%s 353 %s = %s :%s\r\n", "serverName", client.nickname, channel.name, nicknameList)))
	client.conn.Write([]byte(fmt.Sprintf(":%s 366 %s %s End of /NAMES list.\r\n", "serverName", client.nickname, channel.name)))
	client.conn.Write([]byte(fmt.Sprintf(":%s 331 %s %s No topic is set\r\n", "serverName", client.nickname, channel.name)))
}

func handleCap(client *Client, params string) {
	parts := strings.SplitN(params, " ", 2)
	if len(parts) > 1 {
		subCommand := parts[0]
		switch subCommand {
		case "LS":
			client.conn.Write([]byte("CAP * LS :\r\n"))
		case "REQ":
			client.conn.Write([]byte("CAP * NAK :\r\n"))
		case "END":
			client.conn.Write([]byte("CAP * END\r\n"))
		}
	}
}
