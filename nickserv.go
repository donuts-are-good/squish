package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// IRC numeric replies
const (
	RPL_NOTICE = "NOTICE"
)

const NickServNick = "NickServ"

func handleNickServMessage(client *Client, message string) {
	log.Printf("NickServ received message from %s: %s", client.Nickname, message)
	parts := strings.Fields(message)
	if len(parts) < 1 {
		sendNickServHelp(client)
		return
	}

	command := strings.ToUpper(parts[0])
	switch command {
	case "IDENTIFY":
		handleNickServIdentify(client, parts[1:])
	case "REGISTER":
		handleNickServRegister(client, parts[1:])
	case "SET":
		if len(parts) > 1 && strings.ToUpper(parts[1]) == "PASSWORD" {
			handleNickServSetPassword(client, parts[2:])
		} else {
			sendNickServHelp(client)
		}
	case "INFO":
		handleNickServInfo(client, parts[1:])
	case "GHOST":
		handleNickServGhost(client, parts[1:])
	default:
		sendNickServMessage(client, fmt.Sprintf("Unknown command: %s", command))
		sendNickServHelp(client)
	}
}

func sendNickServHelp(client *Client) {
	sendNickServMessage(client, "Available commands:")
	sendNickServMessage(client, "REGISTER <password> <email> - Register your nickname")
	sendNickServMessage(client, "IDENTIFY <nickname> <password> - Identify with a nickname")
	sendNickServMessage(client, "SET PASSWORD <new_password> - Change your password")
	sendNickServMessage(client, "INFO <nickname> - Get information about a nickname")
	sendNickServMessage(client, "GHOST <nickname> <password> - Disconnect an old session")
}

func handleNickServRegister(client *Client, args []string) {
	if len(args) < 2 {
		sendNickServMessage(client, "Syntax: REGISTER <password> <email>")
		return
	}

	password, email := args[0], args[1]

	// Check if the nickname is already registered
	existingClient, err := getClientByNickname(client.Nickname)
	if err == nil && existingClient != nil && existingClient.Password != "" {
		sendNickServMessage(client, "This nickname is already registered.")
		return
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		sendNickServMessage(client, "Error registering nickname")
		return
	}

	// Update the client in the database
	client.Password = string(hashedPassword)
	client.Email = email
	err = updateClientInfo(client)
	if err != nil {
		log.Printf("Error updating client: %v", err)
		sendNickServMessage(client, "Error registering nickname")
		return
	}

	log.Printf("Nickname %s registered successfully with password hash: %s", client.Nickname, client.Password)
	sendNickServMessage(client, fmt.Sprintf("Nickname %s registered successfully", client.Nickname))
	sendNickServMessage(client, "You can now identify using /msg NickServ IDENTIFY <password>")
}

func handleNickServIdentify(client *Client, args []string) {
	log.Printf("NickServ: Handling IDENTIFY command for %s", client.Nickname)
	if len(args) < 2 {
		sendNickServMessage(client, "Syntax: IDENTIFY <nickname> <password>")
		return
	}

	targetNick, password := args[0], args[1]

	// bcrypt the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		sendNickServMessage(client, "Error identifying nickname")
		return
	}

	existingClient, err := getClientByNickname(targetNick)
	if err != nil {
		if err == sql.ErrNoRows {
			sendNickServMessage(client, fmt.Sprintf("The nickname %s is not registered.", targetNick))
		} else {
			log.Printf("NickServ: Error fetching client from database: %v", err)
			sendNickServMessage(client, "Error identifying nickname")
		}
		return
	}

	log.Printf("Attempting to verify password for %s", targetNick)

	if verifyPassword(existingClient.Password, string(hashedPassword)) {
		// If the client is using a different nickname, change it
		if client.Nickname != targetNick {
			oldNickname := client.Nickname
			client.Nickname = targetNick
			updateConnectedClientNickname(oldNickname, targetNick)
			err := updateClientNickname(client)
			if err != nil {
				log.Printf("Error updating client nickname: %v", err)
				sendNickServMessage(client, "Error updating nickname")
				return
			}
			client.conn.Write([]byte(fmt.Sprintf(":%s NICK %s\r\n", oldNickname, targetNick)))
			notifyNicknameChange(client, oldNickname, targetNick)
		}

		client.ID = existingClient.ID // Ensure the client has the correct ID
		client.IsIdentified = true
		client.LastSeen = time.Now()
		err = updateClientInfo(client)
		if err != nil {
			log.Printf("NickServ: Error updating client info for %s: %v", targetNick, err)
			sendNickServMessage(client, "Error updating client information")
			return
		}
		sendNickServMessage(client, fmt.Sprintf("You are now identified for %s", targetNick))
	} else {
		log.Printf("Password verification failed for this guy %s", targetNick)
		sendNickServMessage(client, "Invalid password for nickname")
	}
}

func handleNickServSetPassword(client *Client, args []string) {
	if len(args) < 1 {
		client.sendNumeric(ERR_NEEDMOREPARAMS, "SET PASSWORD", "Not enough parameters")
		return
	}

	if !client.IsIdentified {
		client.sendNumeric(ERR_NOTREGISTERED, "You must identify with NickServ first")
		return
	}

	newPassword := args[0]

	// Hash the new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		client.sendNumeric(ERR_UNKNOWNERROR, "Error changing password")
		return
	}

	// Update the password in the database
	_, err = DB.Exec("UPDATE users SET password = ? WHERE nickname = ?", string(hashedPassword), client.Nickname)
	if err != nil {
		log.Printf("Error updating client password: %v", err)
		client.sendNumeric(ERR_UNKNOWNERROR, "Error changing password")
		return
	}

	client.Password = string(hashedPassword)
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
	client.sendNumeric(RPL_NOTICE, "NickServ", fmt.Sprintf("Email: %s", targetClient.Email))
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
	message := fmt.Sprintf(":%s %s %s :%s\r\n", ServerNameString, numeric, client.Nickname, strings.Join(params, " "))
	client.conn.Write([]byte(message))
}

func verifyPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		log.Printf("Password verification failed here: %v", err)
	}
	return err == nil
}

func sendNickServMessage(client *Client, message string) {
	client.conn.Write([]byte(fmt.Sprintf(":%s NOTICE %s :%s\r\n", NickServNick, client.Nickname, message)))
}
