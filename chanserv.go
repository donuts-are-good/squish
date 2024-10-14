package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

const ChanServNick = "ChanServ"

func NewChanServ() *ChanServType {
	chanServ := &ChanServType{
		client: &Client{
			Nickname: ChanServNick,
			Username: "ChanServ",
			Hostname: "services.squishirc.com",
			Realname: "Channel Services",
		},
	}
	return chanServ
}

func (cs *ChanServType) HandleMessage(sender *Client, message string) {
	parts := strings.Fields(message)
	if len(parts) < 1 {
		cs.sendHelp(sender)
		return
	}

	command := strings.ToUpper(parts[0])
	switch command {
	case "REGISTER":
		cs.handleRegister(sender, parts[1:])
	case "OP":
		cs.handleOp(sender, parts[1:])
	case "DEOP":
		cs.handleDeop(sender, parts[1:])
	case "SET":
		cs.handleSet(sender, parts[1:])
	case "INFO":
		cs.handleInfo(sender, parts[1:])
	default:
		cs.sendHelp(sender)
	}
}

func (cs *ChanServType) handleRegister(sender *Client, args []string) {
	if len(args) < 1 {
		cs.sendNotice(sender, "Syntax: REGISTER <#channel>")
		return
	}

	channelName := args[0]
	channel, err := getOrCreateChannel(channelName)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error registering channel: %v", err))
		return
	}

	// Check if the sender is in the channel and is an operator
	isOperator, err := isClientChannelOperator(sender, channel)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error checking operator status: %v", err))
		return
	}

	if !isOperator {
		cs.sendNotice(sender, "You must be a channel operator to register the channel.")
		return
	}

	// Set the channel as registered in the database
	err = setChannelRegistered(channel.ID, sender.ID)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error registering channel: %v", err))
		return
	}

	cs.sendNotice(sender, fmt.Sprintf("Channel %s has been registered.", channelName))
}

func (cs *ChanServType) handleOp(sender *Client, args []string) {
	if len(args) < 2 {
		cs.sendNotice(sender, "Syntax: OP <#channel> <nickname>")
		return
	}

	channelName, targetNick := args[0], args[1]
	channel, err := getChannel(channelName)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error: %v", err))
		return
	}

	// Check if the sender has the right to op in this channel
	hasRight, err := cs.hasRightToOp(sender, channel)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error checking op rights: %v", err))
		return
	}

	if !hasRight {
		cs.sendNotice(sender, "You don't have the right to op users in this channel.")
		return
	}

	targetClient, err := getClientByNickname(targetNick)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error: User %s not found.", targetNick))
		return
	}

	err = setClientChannelOperator(targetClient, channel, true)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error setting operator status: %v", err))
		return
	}

	broadcastToChannel(channel, fmt.Sprintf(":%s MODE %s +o %s\r\n", ChanServNick, channelName, targetNick))
	cs.sendNotice(sender, fmt.Sprintf("User %s is now an operator in %s.", targetNick, channelName))
}

func (cs *ChanServType) handleDeop(sender *Client, args []string) {
	if len(args) < 2 {
		cs.sendNotice(sender, "Syntax: DEOP <#channel> <nickname>")
		return
	}

	channelName, targetNick := args[0], args[1]
	channel, err := getChannel(channelName)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error: %v", err))
		return
	}

	// Check if the sender has the right to deop in this channel
	hasRight, err := cs.hasRightToOp(sender, channel)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error checking deop rights: %v", err))
		return
	}

	if !hasRight {
		cs.sendNotice(sender, "You don't have the right to deop users in this channel.")
		return
	}

	targetClient, err := getClientByNickname(targetNick)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error: User %s not found.", targetNick))
		return
	}

	err = setClientChannelOperator(targetClient, channel, false)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error removing operator status: %v", err))
		return
	}

	broadcastToChannel(channel, fmt.Sprintf(":%s MODE %s -o %s\r\n", ChanServNick, channelName, targetNick))
	cs.sendNotice(sender, fmt.Sprintf("User %s is no longer an operator in %s.", targetNick, channelName))
}

func (cs *ChanServType) handleSet(sender *Client, args []string) {
	if len(args) < 3 {
		cs.sendNotice(sender, "Syntax: SET <#channel> <setting> <value>")
		return
	}

	channelName, setting, value := args[0], strings.ToUpper(args[1]), args[2]
	channel, err := getChannel(channelName)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error: %v", err))
		return
	}

	// Check if the sender has the right to change settings in this channel
	hasRight, err := cs.hasRightToOp(sender, channel)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error checking rights: %v", err))
		return
	}

	if !hasRight {
		cs.sendNotice(sender, "You don't have the right to change settings in this channel.")
		return
	}

	switch setting {
	case "TOPIC":
		channel.Topic = value
		_, err = DB.Exec("UPDATE channels SET topic = ? WHERE id = ?", value, channel.ID)
	case "LIMIT":
		limit, err := strconv.Atoi(value)
		if err != nil {
			cs.sendNotice(sender, "Invalid user limit. Please provide a number.")
			return
		}
		channel.UserLimit = limit
		_, err = DB.Exec("UPDATE channels SET user_limit = ? WHERE id = ?", limit, channel.ID)
		if err != nil {
			cs.sendNotice(sender, fmt.Sprintf("Error updating channel setting: %v", err))
			return
		}
	// Add more settings as needed
	default:
		cs.sendNotice(sender, fmt.Sprintf("Unknown setting: %s", setting))
		return
	}

	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error updating channel setting: %v", err))
		return
	}

	cs.sendNotice(sender, fmt.Sprintf("Channel %s setting %s has been updated to: %s", channelName, setting, value))
	broadcastToChannel(channel, fmt.Sprintf(":%s NOTICE %s :%s has changed the channel %s to: %s\r\n", ChanServNick, channelName, sender.Nickname, setting, value))
}

func (cs *ChanServType) handleInfo(sender *Client, args []string) {
	if len(args) < 1 {
		cs.sendNotice(sender, "Syntax: INFO <#channel>")
		return
	}

	channelName := args[0]
	channel, err := getChannel(channelName)
	if err != nil {
		cs.sendNotice(sender, fmt.Sprintf("Error: %v", err))
		return
	}

	cs.sendNotice(sender, fmt.Sprintf("Information for %s:", channelName))
	cs.sendNotice(sender, fmt.Sprintf("Topic: %s", channel.Topic))
	cs.sendNotice(sender, fmt.Sprintf("Created at: %s", channel.CreatedAt.Format(time.RFC1123)))
	cs.sendNotice(sender, fmt.Sprintf("User limit: %d", channel.UserLimit))

	// Get the founder's nickname
	var founderNick string
	if channel.FounderID.Valid {
		err = DB.QueryRow("SELECT nickname FROM users WHERE id = ?", channel.FounderID.Int64).Scan(&founderNick)
		if err != nil {
			log.Printf("Error getting founder nickname: %v", err)
			founderNick = "Unknown"
		}
	} else {
		founderNick = "None (channel not registered)"
	}
	cs.sendNotice(sender, fmt.Sprintf("Founder: %s", founderNick))

	// Get the number of users
	userCount, err := getChannelUserCount(channel.ID)
	if err != nil {
		log.Printf("Error getting channel user count: %v", err)
		userCount = 0
	}
	cs.sendNotice(sender, fmt.Sprintf("Users: %d", userCount))
}

func (cs *ChanServType) sendHelp(client *Client) {
	cs.sendNotice(client, "ChanServ commands:")
	cs.sendNotice(client, "REGISTER <#channel> - Register a channel")
	cs.sendNotice(client, "OP <#channel> <nickname> - Give operator status to a user")
	cs.sendNotice(client, "DEOP <#channel> <nickname> - Remove operator status from a user")
	cs.sendNotice(client, "SET <#channel> <setting> <value> - Change channel settings")
	cs.sendNotice(client, "INFO <#channel> - Get information about a channel")
}

func (cs *ChanServType) sendNotice(client *Client, message string) {
	client.conn.Write([]byte(fmt.Sprintf(":%s NOTICE %s :%s\r\n", ChanServNick, client.Nickname, message)))
}

func (cs *ChanServType) hasRightToOp(sender *Client, channel *Channel) (bool, error) {
	// Check if the sender is the channel founder or has sufficient access
	isFounder, err := isClientChannelFounder(sender, channel)
	if err != nil {
		return false, err
	}
	if isFounder {
		return true, nil
	}

	// Add more checks here if needed (e.g., checking for specific access levels)

	return false, nil
}

// Add these functions to database.go:

func setChannelRegistered(channelID int64, founderID int64) error {
	_, err := DB.Exec("UPDATE channels SET is_registered = ?, founder_id = ? WHERE id = ?", true, founderID, channelID)
	return err
}

func isClientChannelOperator(client *Client, channel *Channel) (bool, error) {
	var isOperator bool
	err := DB.QueryRow("SELECT is_operator FROM user_channels WHERE user_id = ? AND channel_id = ?", client.ID, channel.ID).Scan(&isOperator)
	return isOperator, err
}

func isClientChannelFounder(client *Client, channel *Channel) (bool, error) {
	var founderID int64
	err := DB.QueryRow("SELECT founder_id FROM channels WHERE id = ?", channel.ID).Scan(&founderID)
	if err != nil {
		return false, err
	}
	return founderID == client.ID, nil
}

func setClientChannelOperator(client *Client, channel *Channel, isOperator bool) error {
	_, err := DB.Exec("UPDATE user_channels SET is_operator = ? WHERE user_id = ? AND channel_id = ?", isOperator, client.ID, channel.ID)
	return err
}

func getChannel(name string) (*Channel, error) {
	var channel Channel
	err := DB.Get(&channel, "SELECT * FROM channels WHERE name = ?", name)
	if err != nil {
		return nil, err
	}
	return &channel, nil
}
