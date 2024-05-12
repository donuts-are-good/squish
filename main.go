package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
)

// Client represents a connected client with a nickname and a connection.
type Client struct {
	conn     net.Conn
	nickname string
}

// clients holds all connected clients.
var clients []Client

// main starts the server and handles incoming connections.
func main() {
	ln, err := net.Listen("tcp", ":6667")
	if err != nil {
		log.Fatalln(err)
	}
	defer ln.Close()

	for {
		// Accept a new connection.
		conn, err := ln.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("con: client connected:", conn.RemoteAddr())
		// Handle the connection in a new goroutine.
		go handleConnection(conn)
	}
}

// handleConnection manages the connection for a single client.
func handleConnection(conn net.Conn) {
	defer conn.Close()

	// Initialize a new client with the connection.
	client := Client{conn: conn, nickname: ""}
	reader := bufio.NewReader(conn)

	for {
		// Read a message from the client.
		message, err := reader.ReadString('\n')
		if err != nil {
			log.Println("con: disconnect:", conn.RemoteAddr().String())
			return
		} else {
			log.Println("msg:", message, conn.RemoteAddr().String())
		}

		// Trim and split the message to extract command and parameters.
		message = strings.Trim(message, "\r\n")
		parts := strings.SplitN(message, " ", 2)

		if len(parts) > 1 {
			command, params := parts[0], parts[1]
			switch command {
			case "NICK":
				log.Println("command: nick")
				// Handle nickname setting.
				handleNick(&client, params, conn)
			case "USERS":
				// Handle the /users command.
				log.Println("command: users")
				listUsers(&client)
			}
		}
	}
}

// handleNick sets the nickname for a client by appending a unique 4-digit number.
func handleNick(client *Client, nickname string, conn net.Conn) {
	if len(nickname) > 16 {
		client.conn.Write([]byte("ERROR: Nickname too long.\n"))
		return
	}

	// Maximum attempts to find a unique nickname
	maxAttempts := 100
	attempts := 0

	for attempts < maxAttempts {
		// Generate a random 4-digit number
		randomNumber := rand.Intn(9000) + 1000
		// Append the random number to the nickname
		uniqueNickname := fmt.Sprintf("%s(#%04d)", nickname, randomNumber)

		// Check if the nickname is already in use
		isUnique := true
		for _, c := range clients {
			if c.nickname == uniqueNickname {
				isUnique = false
				break
			}
		}

		if isUnique {
			// If unique, set the nickname and break the loop
			client.nickname = uniqueNickname

			// Remove the old client entry from the clients slice
			for i, c := range clients {
				if c.conn == client.conn {
					clients = append(clients[:i], clients[i+1:]...)
					break
				}
			}

			// Add the updated client to the clients slice
			clients = append(clients, *client)
			client.conn.Write([]byte(fmt.Sprintf("NICK %s\n", uniqueNickname)))
			return
		}

		attempts++
	}

	// If no unique nickname could be found after max attempts, disconnect the client
	client.conn.Write([]byte("ERROR: Unable to assign a unique nickname.\n"))
	conn.Close()
}

// listUsers sends a list of all connected users to the requesting client.
func listUsers(client *Client) {
	log.Println("listusers: starting")
	var users []string
	for _, c := range clients {
		log.Println("listusers: client:", c.nickname)
		users = append(users, c.nickname)
	}
	log.Println("listusers: writing:", users)
	// Convert the slice of nicknames into a single string with each nickname separated by a space
	userList := strings.Join(users, " ")
	// Write the list of users to the client's connection, ensuring the message ends with \r\n
	// Format the message according to IRC protocol
	client.conn.Write([]byte(fmt.Sprintf(":%s 265 %s :%s\r\n", "serverName", client.nickname, userList)))
}
