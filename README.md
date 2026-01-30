# Go-Upkeep

## Features
- **TUI Dashboard**: Terminal interface.
- **SSH Access**: Connect via `ssh -p 23234 server-ip`.
- **SSL Monitoring**: Tracks certificate expiry and warns before it happens.
- **Alerting**: Supports Discord Webhooks for Down/Up/SSL events.

## Production Deployment

### 1. Prepare Host Directories
Create the directories on your host machine to persist the database and server identity.

```bash
sudo mkdir -p /mnt/upkeep/data
sudo mkdir -p /mnt/upkeep/ssh_host_keys
```

### 2. Configure Access
You must whitelist your SSH Public Key. The server uses strict authentication and will deny connections if this file is missing.

**On your local machine** (where you will connect *from*), get your public key.

**On the server**, create the `authorized_keys` file in the data directory:
```bash
# Paste public key into this file
echo "ssh-ed25519 AAAAC3Nza..." > /mnt/docker-volumes/upkeep/data/authorized_keys
```

### 3. Docker Compose
Create a `docker-compose.yml` file:

```yaml
services:
  monitor:
    image: rdgames1000/go-upkeep:latest
    container_name: go-upkeep
    restart: unless-stopped
    ports:
      - "23234:23234"
    volumes:
      # Data Volume: Stores the SQLite DB and authorized_keys file
      - /mnt/docker-volumes/upkeep/data:/data
      
      # Identity Volume: Persists the server's Host Key so it doesn't change on restart
      - /mnt/docker-volumes/upkeep/ssh_host_keys:/app/.ssh
```

### 4. Start the Service
```bash
docker compose up -d
```

### 5. Connect
```bash
ssh -p 23234 user@your-server-ip
```

## Usage

### Navigation
- **n**: New Site / Alert
- **d**: Delete
- **e** or **Enter**: Edit
- **Tab**: Switch between Sites, Alerts, and Logs tabs.
- **PgUp/PgDn**: Scroll Logs or Forms.

## Areas of improvement
- Public https dashboard
- More aletr types (email SMTP, )
- Push monitor
- Optional postgress support