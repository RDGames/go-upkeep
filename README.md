# Go-Upkeep

## Features
- **TUI Dashboard**: Terminal interface.
- **SSH Access**: Connect via `ssh -p 23234 server-ip`.
- **SSL Monitoring**: Tracks certificate expiry and warns before it happens.
- **Alerting**: Supports Discord Webhooks for Down/Up/SSL events.

## Local Development

If you want to run the app without Docker:

1.  **Prerequisites:** Install Go 1.25.
2.  **Setup:**
    The app requires an `authorized_keys` file in the project root to authenticate SSH connections.
    ```bash
    # Copy your public key to the project root
    cat ~/.ssh/key.pub > authorized_keys
    ```
3.  **Run the App:**
    ```bash
    go mod tidy
    go run cmd/goupkeep/main.go
    ```
    *The TUI will open immediately in terminal.*

4.  **Test SSH Access:**
    Open a second terminal window:
    ```bash
    ssh -p 23234 localhost
    ```

## Production Deployment

1. Prepare Host Directories
Create the directories on your host machine to persist the database and server identity.

```bash
sudo mkdir -p /mnt/upkeep/data
sudo mkdir -p /mnt/upkeep/ssh_host_keys
```

2. Configure Access
You must whitelist your SSH Public Key. The server uses strict authentication and will deny connections if this file is missing.

**On your local machine** (where you will connect *from*), get your public key.

**On the server**, create the `authorized_keys` file in the data directory:
```bash
# Paste public key into this file
echo "ssh-ed25519 AAAAC3Nza..." > /mnt/upkeep/data/authorized_keys
```

3. Docker Compose
Create a `docker-compose.yml` file:

```yaml
services:
  monitor:
    image: rdgames1000/go-upkeep:latest
    container_name: go-upkeep
    restart: unless-stopped
    stdin_open: true
    tty: true
    ports:
      - "23234:23234"
    volumes:
      # Data Volume: Stores the SQLite DB and authorized_keys file
      - /mnt/upkeep/data:/data
      
      # Identity Volume: Persists the server's Host Key so it doesn't change on restart
      - /mnt/upkeep/ssh_host_keys:/app/.ssh
```

4. Start the Service
```bash
docker compose up -d
```

## Accessing the Dashboard

### Method A: SSH
This creates a remote session.
```bash
ssh -p 23234 user@your-server-ip
```

### Method B: Docker Attach (Local Console)
You can view the main process directly on the server console.

1.  **Attach:**
    ```bash
    docker attach go-upkeep
    ```
2.  **Detach:**
    To leave the console *without* stopping the container, use the standard Docker detach sequence:
    **Press `Ctrl+P` followed by `Ctrl+Q`**.

    *If you press `q` or `Ctrl+C` while attached, you will terminate the container process.*

## Usage

### Navigation
- **n**: New Site / Alert
- **d**: Delete
- **e** or **Enter**: Edit
- **Tab**: Switch between Sites, Alerts, and Logs tabs.
- **PgUp/PgDn**: Scroll Logs or Forms.

## Areas of improvement
- Public https dashboard
- More alert types (email SMTP, )
- Push monitor
- Optional postgress support