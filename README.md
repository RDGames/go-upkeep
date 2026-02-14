# Go-Upkeep

![Go Version](https://img.shields.io/badge/go-1.23-blue) ![License](https://img.shields.io/badge/license-MIT-green) ![Docker](https://img.shields.io/docker/pulls/rdgames1000/go-upkeep)

**Go-Upkeep** is a self-hosted infrastructure monitor with a retro-futuristic TUI accessible via SSH. It supports High Availability, Push Monitoring, and Alerting.

*   🌐 **Full Documentation:** [goupkeep.org/docs](https://goupkeep.org/docs)
*   🐳 **Docker Hub:** [rdgames1000/go-upkeep](https://hub.docker.com/r/rdgames1000/go-upkeep)

---

## 🚀 Key Features

*   **SSH Dashboard**: Zero-install client. Manage monitors via `ssh -p 23234 your-server`.
*   **Protocols**:
    *   **HTTP/S**: Active polling with SSL certificate expiration tracking.
    *   **PUSH**: Heartbeat endpoints for cron jobs/backup scripts.
*   **High Availability**: Leader/Follower clustering with automatic failover.
*   **Alerting**: Native support for Discord, Slack, Email (SMTP), and Webhooks.
*   **Backends**: SQLite (default) or PostgreSQL (production).

---

## 🛠️ Quick Start (Local Dev)

**Option A: Native Go (Fastest)**
```bash
go mod tidy
go run cmd/goupkeep/main.go
# Connect: ssh -p 23234 localhost
```

**Option B: Docker Compose (Full Stack)**
```bash
docker compose -f docker-compose.dev.yml up --build
```

---

## 📦 Production Deployment

For critical infrastructure, we recommend Docker Compose.

### 1. The Compose File
Create `docker-compose.yml`:

```yaml
services:
  monitor:
    image: rdgames1000/go-upkeep:latest
    container_name: go-upkeep
    restart: unless-stopped
    stdin_open: true # Required for initial setup console
    tty: true
    ports:
      - "23234:23234" # SSH
      - "8080:8080"   # HTTP (Status Page & Push)
    volumes:
      - ./data:/data
      - ./ssh_keys:/app/.ssh
    environment:
      - UPKEEP_DB_TYPE=sqlite
      - UPKEEP_DB_DSN=/data/upkeep.db
      - UPKEEP_STATUS_ENABLED=true
      - UPKEEP_CLUSTER_SECRET=ChangeMeToSomethingSecure
```

### 2. Initial Setup (Identity Management)
**Important:** V2 stores SSH keys in the database. You must create the first user manually via the console.

1.  Start the stack: `docker compose up -d`
2.  Attach to the container: `docker attach go-upkeep`
3.  Inside the TUI:
    *   Press **[Tab]** to select the `Users` tab.
    *   Press **[n]** to create a user.
    *   Enter your username and paste your public key (`cat ~/.ssh/id_ed25519.pub`).
    *   Press **[Enter]** to save.
4.  Detach: Press **Ctrl+P** then **Ctrl+Q**.

### 3. Usage
Connect using your standard SSH client:
```bash
ssh -p 23234 your-server-ip
```

For advanced setups (Postgres, Clustering, Migration), please consult the [Official Documentation](https://goupkeep.org/docs).

## 📄 License
MIT License.