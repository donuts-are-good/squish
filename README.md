# SQUISH IRC Server

SQUISH is a lightweight IRC server implemented in Go. It provides basic IRC functionality with support for nickname registration, channel management, and various IRC commands.

## Features

- Basic IRC protocol implementation
- Nickname registration and authentication (NickServ)
- Channel registration and management (ChanServ)
- Support for standard IRC commands
- SQLite database for persistent storage

## Requirements

- Go 1.16 or higher
- SQLite3

## Installation

1. Clone the repository:   ```
   git clone https://github.com/donuts-are-good/squish.git   ```

2. Navigate to the project directory:   ```
   cd squish   ```

3. Install dependencies:   ```
   go mod tidy   ```

4. Build the server:   ```
   go build   ```

## Usage

1. Start the server:   ```
   ./squish   ```

   The server will start on port 6667 by default.

2. Connect to the server using an IRC client of your choice.

## Connecting to the Server

Use any standard IRC client to connect to the SQUISH server. Here are some examples:

### Using irssi

1. Open irssi
2. Connect to the server:   ```
   /connect localhost 6667   ```
3. Set your nickname:   ```
   /nick YourNickname   ```
4. Register your nickname:   ```
   /msg NickServ REGISTER YourPassword your@email.com   ```

### Using HexChat

1. Open HexChat
2. Add a new network:
   - Network name: SQUISH
   - Server: localhost/6667
3. Connect to the server
4. Register your nickname:   ```
   /msg NickServ REGISTER YourPassword your@email.com   ```

## Implemented Commands

- NICK: Change nickname
- USER: Set user information
- JOIN: Join channels
- PART: Leave channels
- PRIVMSG: Send messages
- QUIT: Disconnect from server
- LIST: List channels
- NAMES: List users in a channel
- TOPIC: View or set channel topic
- MODE: Set or remove channel/user modes
- WHO: List information about users
- WHOIS: Get detailed user information
- KICK: Kick a user from a channel
- BAN: Ban a user from a channel
- UNBAN: Remove a ban from a channel
- BANLIST: List all bans in a channel

## NickServ Commands

- REGISTER: Register a nickname
- IDENTIFY: Identify with a registered nickname
- SET PASSWORD: Change password
- INFO: Get nickname information
- GHOST: Disconnect an old session

## ChanServ Commands

- REGISTER: Register a channel
- OP: Give operator status
- DEOP: Remove operator status
- SET: Change channel settings
- INFO: Get channel information

## Supported Modes

### User Modes
- +i: Set user as invisible

### Channel Modes
- +n: No external messages (only channel members can send messages)
- +t: Only channel operators can change the topic
- +m: Moderated channel (only voiced users and operators can speak)
- +i: Invite-only channel
- +k <key>: Set a channel key (password)
- +l <limit>: Set a user limit for the channel
- +b <mask>: Set a ban on the channel
- +o <nickname>: Give channel operator status to a user
- +v <nickname>: Give voice status to a user

## Configuration

The server uses a SQLite database (`irc.db`) for persistent storage. The database is created automatically on first run.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License.
