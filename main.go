package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"time"
	"unicode"
)

type Client struct {
	conn     net.Conn
	nickname string
	channels []*Channel
}

type Channel struct {
	name    string
	clients []*Client
	topic   string
}

var clients []Client

var channels []*Channel

func main() {
	ln, err := net.Listen("tcp", ":6667")
	if err != nil {
		log.Fatalln(err)
	}
	defer ln.Close()

	channels = append(channels, &Channel{name: "#general", topic: "Welcome to #general"})
	channels = append(channels, &Channel{name: "#help", topic: "Welcome to #help"})

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("con: client connected:", conn.RemoteAddr())

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	client := Client{conn: conn, nickname: ""}
	reader := bufio.NewReader(conn)

	pingTicker := time.NewTicker(1 * time.Minute)
	defer pingTicker.Stop()

	for {
		select {
		case <-pingTicker.C:
			conn.Write([]byte(fmt.Sprintf("PING :%s\r\n", "serverName")))

		default:
			message, err := reader.ReadString('\n')
			if err != nil {
				switch e := err.(type) {
				case net.Error:
					if e.Timeout() {
						log.Println("con: timeout:", conn.RemoteAddr().String())
					} else {
						log.Println("con: disconnect:", conn.RemoteAddr().String())
					}
					removeClient(&client)
					return
				default:
					log.Println("con: disconnect:", conn.RemoteAddr().String())
					removeClient(&client)
					return
				}
			} else {
				log.Println("msg:", message, conn.RemoteAddr().String())
			}

			message = strings.Trim(message, "\r\n")
			parts := strings.SplitN(message, " ", 2)

			if len(parts) > 1 {
				command, params := parts[0], parts[1]
				switch command {
				case "PING":
					conn.Write([]byte(fmt.Sprintf("PONG %s\r\n", params)))
				case "PONG":

					conn.SetReadDeadline(time.Now().Add(3 * time.Minute))
				case "NICK":
					log.Println("command: nick")
					sanitizedNickname := sanitizeString(params)
					handleNick(&client, sanitizedNickname, conn)
				case "NAMES":
					log.Println("command: names")
					targetAndChannel := strings.SplitN(params, " ", 2)
					log.Println("targetAndChannel:", targetAndChannel)
					var channelName string
					if len(targetAndChannel) > 1 {
						channelName = sanitizeString(targetAndChannel[1])
					}
					handleNames(&client, channelName)

				case "JOIN":
					log.Println("command: join")
					sanitizedChannelName := sanitizeString(params)
					handleJoin(&client, sanitizedChannelName)
				case "LIST":
					log.Println("command: list")
					handleList(&client)
				case "PRIVMSG":
					log.Println("command: privmsg")
					targetAndMessage := strings.SplitN(params, " ", 2)
					if len(targetAndMessage) > 1 {
						target, message := targetAndMessage[0], targetAndMessage[1]
						handlePrivmsg(&client, target, message)
					}
					conn.SetReadDeadline(time.Now().Add(3 * time.Minute))
				}
			}
		}
	}
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

func broadcastMessage(channel *Channel, sender *Client, message string) {
	for _, client := range channel.clients {
		if client != sender {
			client.conn.Write([]byte(fmt.Sprintf(":%s PRIVMSG %s %s\r\n", sender.nickname, channel.name, message)))
		}
	}
}

func handleList(client *Client) {
	log.Println("handleList: start")
	for _, channel := range channels {
		log.Println("handleList: channel name:", channel.name)

		userCount := len(channel.clients)

		client.conn.Write([]byte(fmt.Sprintf(":%s 322 %s %s %d %s\r\n", "serverName", client.nickname, channel.name, userCount, channel.topic)))
	}
}

func handleNames(client *Client, channelName string) {
	log.Println("handleUsers: starting")
	var users []string

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
		for _, c := range clients {
			if c.nickname == uniqueNickname {
				isUnique = false
				break
			}
		}

		if isUnique {
			client.nickname = uniqueNickname

			for i, c := range clients {
				if c.conn == client.conn {
					clients = append(clients[:i], clients[i+1:]...)
					break
				}
			}

			clients = append(clients, *client)
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

	channel := findChannel(channelName)
	if channel == nil {
		channel = &Channel{name: channelName, topic: "Welcome to " + channelName}
		channels = append(channels, channel)
	}
	channel.clients = append(channel.clients, client)
	client.channels = append(client.channels, channel)

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

func findClientByNickname(nickname string) *Client {
	for _, client := range clients {
		if client.nickname == nickname {
			return &client
		}
	}
	return nil
}

func findChannel(name string) *Channel {
	for _, channel := range channels {
		if channel.name == name {
			return channel
		}
	}
	return nil
}

func removeClient(client *Client) {
	for i, c := range clients {
		if c.conn == client.conn {
			clients = append(clients[:i], clients[i+1:]...)
			break
		}
	}
	for _, channel := range client.channels {
		removeClientFromChannel(channel, client)
	}
}

func removeClientFromChannel(channel *Channel, client *Client) {
	for i, c := range channel.clients {
		if c.conn == client.conn {
			channel.clients = append(channel.clients[:i], channel.clients[i+1:]...)
			break
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
