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
	defer conn.Close()

	client := &Client{conn: conn, nickname: ""}
	reader := bufio.NewReader(conn)

	mu.Lock()
	clients = append(clients, client)
	mu.Unlock()

	pingTicker := time.NewTicker(1 * time.Minute)
	defer pingTicker.Stop()

	for {
		select {
		case <-pingTicker.C:
			conn.Write([]byte(fmt.Sprintf("PING :%s\r\n", ServerNameString)))

		default:
			message, err := reader.ReadString('\n')
			if err != nil {
				handleDisconnect(client, err)
				return
			}

			log.Println("msg:", message, conn.RemoteAddr().String())
			message = strings.Trim(message, "\r\n")
			parts := strings.SplitN(message, " ", 2)

			if len(parts) > 1 {
				command, params := parts[0], parts[1]
				switch command {
				case "PING":
					conn.Write([]byte(fmt.Sprintf("PONG %s\r\n", params)))
				case "PONG":
					log.Println("PONG!")
				case "NICK":
					log.Println("command: nick")
					sanitizedNickname := sanitizeString(params)
					handleNick(client, sanitizedNickname, conn)
					sendWelcomeMessages(client)
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
					sanitizedChannelName := sanitizeString(params)
					handleJoin(client, sanitizedChannelName)
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
				case "CAP":
					handleCap(client, params)
				case "MOTD":
					sendMotd(client)
				}
			}
		}
	}
}
