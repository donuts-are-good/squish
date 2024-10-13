package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// IRC numeric replies
const (
	RPL_NOTICE         = "NOTICE"
	ERR_NEEDMOREPARAMS = "461"
	ERR_NICKNAMEINUSE  = "433"
	ERR_UNKNOWNERROR   = "400"
	ERR_NOSUCHNICK     = "401"
	ERR_PASSWDMISMATCH = "464"
	ERR_NOTREGISTERED  = "451"
)

func handleNickServMessage(client *Client, message string) {
	parts := strings.Fields(message)
	if len(parts) < 1 {
		sendNickServHelp(client)
		return
	}

	command := strings.ToUpper(parts[0])
	switch command {
	case "REGISTER":
		handleNickServRegister(client, parts[1:])
	case "IDENTIFY":
		handleNickServIdentify(client, parts[1:])
	case "SET":
		handleNickServSet(client, parts[1:])
	case "INFO":
		handleNickServInfo(client, parts[1:])
	case "GHOST":
		handleNickServGhost(client, parts[1:])
	default:
		sendNickServHelp(client)
	}
}

func sendNickServHelp(client *Client) {
	client.sendNumeric(RPL_NOTICE, "NickServ", "Available commands:")
	client.sendNumeric(RPL_NOTICE, "NickServ", "REGISTER <password> <email> - Register your nickname")
	client.sendNumeric(RPL_NOTICE, "NickServ", "IDENTIFY <password> - Identify with your nickname")
	client.sendNumeric(RPL_NOTICE, "NickServ", "SET PASSWORD <new_password> - Change your password")
	client.sendNumeric(RPL_NOTICE, "NickServ", "INFO <nickname> - Get information about a nickname")
	client.sendNumeric(RPL_NOTICE, "NickServ", "GHOST <nickname> <password> - Disconnect an old session")
}

func handleNickServRegister(client *Client, args []string) {
	if len(args) < 2 {
		client.sendNumeric(ERR_NEEDMOREPARAMS, "REGISTER", "Not enough parameters")
		return
	}

	password, email := args[0], args[1]

	// Check if the nickname is already registered
	existingClient, err := getClientByNickname(client.Nickname)
	if err == nil && existingClient != nil {
		client.sendNumeric(ERR_NICKNAMEINUSE, client.Nickname, "Nickname is already registered")
		return
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		client.sendNumeric(ERR_UNKNOWNERROR, "Error registering nickname")
		return
	}

	// Create the client in the database
	client.Password = string(hashedPassword)
	client.CreatedAt = time.Now()
	client.LastSeen = time.Now()
	// Add the email to the client struct
	client.Email = email
	err = createClient(client)
	if err != nil {
		log.Printf("Error creating client: %v", err)
		client.sendNumeric(ERR_UNKNOWNERROR, "Error registering nickname")
		return
	}

	client.sendNumeric(RPL_NOTICE, "NickServ", fmt.Sprintf("Nickname %s registered successfully", client.Nickname))
}

func handleNickServIdentify(client *Client, args []string) {
	if len(args) < 1 {
		client.sendNumeric(ERR_NEEDMOREPARAMS, "IDENTIFY", "Not enough parameters")
		return
	}

	password := args[0]

	existingClient, err := getClientByNickname(client.Nickname)
	if err != nil {
		client.sendNumeric(ERR_NOSUCHNICK, client.Nickname, "This nickname is not registered")
		return
	}

	if verifyPassword(existingClient.Password, password) {
		client.IsIdentified = true
		client.LastSeen = time.Now()
		err = updateClientInfo(client)
		if err != nil {
			log.Printf("Error updating client info: %v", err)
		}
		client.sendNumeric(RPL_NOTICE, "NickServ", fmt.Sprintf("You are now identified for %s", client.Nickname))
	} else {
		client.sendNumeric(ERR_PASSWDMISMATCH, "Invalid password for nickname")
	}
}

func handleNickServSet(client *Client, args []string) {
	if len(args) < 2 || strings.ToUpper(args[0]) != "PASSWORD" {
		client.sendNumeric(ERR_NEEDMOREPARAMS, "SET", "Invalid SET command. Use SET PASSWORD <new_password>")
		return
	}

	if !client.IsIdentified {
		client.sendNumeric(ERR_NOTREGISTERED, "You must identify with NickServ first")
		return
	}

	newPassword := args[1]

	// Hash the new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		client.sendNumeric(ERR_UNKNOWNERROR, "Error changing password")
		return
	}

	// Update the password in the database
	client.Password = string(hashedPassword)
	err = updateClientInfo(client)
	if err != nil {
		log.Printf("Error updating client password: %v", err)
		client.sendNumeric(ERR_UNKNOWNERROR, "Error changing password")
		return
	}

	client.sendNumeric(RPL_NOTICE, "NickServ", "Password changed successfully")
}

func handleNickServInfo(client *Client, args []string) {
	if len(args) < 1 {
		client.sendNumeric(ERR_NEEDMOREPARAMS, "INFO", "Not enough parameters")
		return
	}

	targetNick := args[0]
	targetClient, err := getClientByNickname(targetNick)
	if err != nil {
		client.sendNumeric(ERR_NOSUCHNICK, targetNick, "No such nickname")
		return
	}

	client.sendNumeric(RPL_NOTICE, "NickServ", fmt.Sprintf("Information for %s:", targetNick))
	client.sendNumeric(RPL_NOTICE, "NickServ", fmt.Sprintf("Registered on: %s", targetClient.CreatedAt.Format(time.RFC1123)))
	client.sendNumeric(RPL_NOTICE, "NickServ", fmt.Sprintf("Last seen: %s", targetClient.LastSeen.Format(time.RFC1123)))
}

func handleNickServGhost(client *Client, args []string) {
	if len(args) < 2 {
		client.sendNumeric(ERR_NEEDMOREPARAMS, "GHOST", "Not enough parameters")
		return
	}

	targetNick, password := args[0], args[1]
	targetClient, err := getClientByNickname(targetNick)
	if err != nil {
		client.sendNumeric(ERR_NOSUCHNICK, targetNick, "No such nickname")
		return
	}

	if !verifyPassword(targetClient.Password, password) {
		client.sendNumeric(ERR_PASSWDMISMATCH, "Invalid password for nickname")
		return
	}

	// Find the connected client with the target nickname
	connectedClient := findClientByNickname(targetNick)
	if connectedClient != nil {
		// Disconnect the old session
		connectedClient.sendNumeric(RPL_NOTICE, "NickServ", "This nickname has been ghosted")
		handleQuit(connectedClient, "Ghosted")
	}

	client.sendNumeric(RPL_NOTICE, "NickServ", fmt.Sprintf("Ghost with nickname %s has been disconnected", targetNick))
}

// Helper functions

func (client *Client) sendNumeric(numeric string, params ...string) {
	message := fmt.Sprintf(":%s %s %s %s\r\n", ServerNameString, numeric, client.Nickname, strings.Join(params, " "))
	client.conn.Write([]byte(message))
}

// Note: These functions are already defined in main.go, so we don't need to redefine them here.
// They are left as comments for reference.

// func createClient(client *Client) error {
// 	// Implementation in main.go
// }

// func updateClientInfo(client *Client) error {
// 	// Implementation in main.go
// }

// func verifyPassword(hashedPassword, password string) bool {
// 	// Implementation in main.go
// }
