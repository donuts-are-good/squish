package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
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

var (
	clients  []*Client
	channels []*Channel
	mu       sync.Mutex
)

func main() {

	log.Println("Starting Squish on 6667")
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

func broadcastMessage(channel *Channel, sender *Client, message string) {
	for _, client := range channel.clients {
		if client != sender {
			client.conn.Write([]byte(fmt.Sprintf(":%s PRIVMSG %s %s\r\n", sender.nickname, channel.name, message)))
		}
	}
}

func sendMotd(client *Client) {
	client.conn.Write([]byte(fmt.Sprintf(":%s 375 %s :- [] %s Message of the day - [] \r\n", "serverName", client.nickname, "serverName")))
	client.conn.Write([]byte(fmt.Sprintf(":%s 372 %s :- [] Welcome to the IRC server!\r\n", "serverName", client.nickname)))
	client.conn.Write([]byte(fmt.Sprintf(":%s 376 %s :End of /MOTD command.\r\n", "serverName", client.nickname)))
}

func sendWelcomeMessages(client *Client) {
	client.conn.Write([]byte(fmt.Sprintf(":%s 001 %s :Welcome to the IRC server\r\n", "serverName", client.nickname)))
	client.conn.Write([]byte(fmt.Sprintf(":%s 002 %s :Your host is %s, running version 1.0\r\n", "serverName", client.nickname, "serverName")))
	client.conn.Write([]byte(fmt.Sprintf(":%s 003 %s :This server was created at some point\r\n", "serverName", client.nickname)))
	client.conn.Write([]byte(fmt.Sprintf(":%s 004 %s %s 1.0 o o\r\n", "serverName", client.nickname, "serverName")))
	sendMotd(client)
}

func removeClient(client *Client) {
	mu.Lock()
	defer mu.Unlock()
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

func findClientByNickname(nickname string) *Client {
	mu.Lock()
	defer mu.Unlock()
	for _, client := range clients {
		if client.nickname == nickname {
			return client
		}
	}
	return nil
}

func findChannel(name string) *Channel {
	mu.Lock()
	defer mu.Unlock()
	for _, channel := range channels {
		if channel.name == name {
			return channel
		}
	}
	return nil
}
