## Comprehensive list of commands

### Connection Registration
- [ ] PASS: Used to set a connection password.
- [ ] NICK: Sets or changes the nickname of a user.
- [ ] USER: Used at the beginning of a connection to specify the username, hostname, servername, and real name of a new user.
- [ ] OPER: Used by a normal user to obtain operator privileges.
- [ ] MODE: Changes the mode of a user or channel.
- [ ] SERVICE: Registers a new service with the server.
- [ ] QUIT: Terminates a connection to the server.
- [ ] SQUIT: Used to disconnect a server link.

### Channel Operations
- [ ] JOIN: Joins a user to a channel.
- [ ] PART: Causes a user to leave a channel.
- [ ] MODE: Changes the mode of a channel or user.
- [ ] TOPIC: Sets or retrieves the topic of a channel.
- [ ] NAMES: Lists all users in a channel.
- [ ] LIST: Lists all channels and their topics.
- [ ] INVITE: Invites a user to a channel.
- [ ] KICK: Removes a user from a channel.

### Server Queries and Commands
- [ ] VERSION: Returns the version of the server.
- [ ] STATS: Provides server statistics.
- [ ] LINKS: Lists all servers connected to the network.
- [ ] TIME: Returns the local time of the server.
- [ ] CONNECT: Connects to another server.
- [ ] TRACE: Traces the route to a server.
- [ ] ADMIN: Returns information about the server's administrators.
- [ ] INFO: Returns information about the server.

### Sending Messages
- [ ] PRIVMSG: Sends a private message to a user or channel.
- [ ] NOTICE: Sends a notice to a user or channel (similar to PRIVMSG but typically not requiring an automated response).

### User-based Queries
- [ ] WHO: Lists information about users matching a certain criteria.
- [ ] WHOIS: Returns information about a specific user.
- [ ] WHOWAS: Returns information about a user who is no longer online.

### Miscellaneous Messages
- [ ] KILL: Disconnects a user from the server.
- [ ] PING: Tests the connection to another server or user.
- [ ] PONG: Replies to a PING message to confirm the connection is still active.
- [ ] ERROR: Reports an error to the client.

### Optional Commands (Commonly Supported)
- [ ] AWAY: Sets an away message for a user.
- [ ] REHASH: Reloads the server configuration file.
- [ ] DIE: Shuts down the server.
- [ ] RESTART: Restarts the server.
- [ ] SUMMON: Summons a user to IRC (often not implemented due to security concerns).
- [ ] USERS: Lists users logged into the server (often not implemented due to privacy concerns).
- [ ] WALLOPS: Sends a message to all operators.
- [ ] USERHOST: Returns the hostnames of specified users.
- [ ] ISON: Checks if specified users are online.
