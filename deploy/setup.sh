#!/bin/bash
set -e

# Target server (no longer hardcoded - confirm before use)
DEPLOY_HOST="${DEPLOY_HOST:-root@YOUR_SERVER_IP}"

echo "========================================="
echo "  Kada one-shot server deployment script"
echo "  Target: $DEPLOY_HOST"
echo "========================================="

# ========== 1. Update system + install base tools ==========
echo "[1/6] Updating the system..."
apt update -y && apt upgrade -y
apt install -y curl wget git vim ufw htop ca-certificates gnupg lsb-release

# ========== 2. Install Docker ==========
echo "[2/6] Installing Docker..."
curl -fsSL https://mirrors.aliyun.com/docker-ce/linux/ubuntu/gpg | gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://mirrors.aliyun.com/docker-ce/linux/ubuntu $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
apt update -y
apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
systemctl enable docker --now

# ========== 3. Create project directory + clone code ==========
echo "[3/6] Fetching the project code..."
mkdir -p /opt/kada
cd /opt/kada

# Option A: scp from your dev machine (recommended - keeps .env and other config)
# Run this on your dev machine first: scp -r /home/chun/dev/projects/kada/* "$DEPLOY_HOST":/opt/kada/

# Option B: use git instead (if a repo exists)
# git clone https://github.com/yourusername/kada.git /opt/kada

echo "Upload the code from your dev machine with scp:"
echo "  scp -r /home/chun/dev/projects/kada/. \"$DEPLOY_HOST\":/opt/kada/"
echo "  # One copy carries everything: the Go module is the repository root, and web/, deploy/ and"
echo "  # docker-compose.yml sit beside it."

# ========== 4. Configure firewall ==========
echo "[4/6] Configuring the firewall..."
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

# ========== 5. Start services ==========
echo "[5/6] Starting the Docker services..."
# cd /opt/kada && docker compose up -d

# ========== 6. Check status ==========
echo "[6/6] Checking service status..."
docker --version
docker compose version

echo ""
echo "========================================="
echo "  Base environment installed!"
echo "  Next:"
echo "  1. scp the project code to /opt/kada/"
echo "  2. cd /opt/kada && docker compose up -d"
echo "  3. curl http://localhost:8080/api/health"
echo "========================================="
