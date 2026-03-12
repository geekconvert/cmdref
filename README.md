```
mkdir cmdref && cd cmdref
go mod init cmdref
nano main.go
```

```
go build -o cmdref .
sudo mv cmdref /usr/local/bin/cmdref
cmdref list
```

```
# Build a universal macOS binary
rm -f cmdref*
GOOS=darwin GOARCH=amd64 go build -o cmdref_amd64 .
#This binary will run on

MacBook Pro (Intel)

Older Intel-based Macs

📌 If you run this binary on Apple Silicon without Rosetta, it will fail.
GOOS=darwin GOARCH=arm64 go build -o cmdref_arm64 .
#This binary will run on

M1 / M2 / M3 Macs

Native speed, no emulation

lipo -create -output cmdref cmdref_amd64 cmdref_arm64
🔥 This is the magic step

lipo is a macOS-only tool that creates a Universal Binary.

What it does conceptually

It packs two binaries into one file:

cmdref
 ├─ Intel (amd64) binary
 └─ Apple Silicon (arm64) binary

At runtime

macOS:

Detects your CPU

Automatically runs the correct architecture

You don’t do anything — macOS handles it.
chmod +x cmdref
```

```
# package it
VERSION=0.1.0
tar -czf cmdref-${VERSION}-darwin-universal.tar.gz cmdref

shasum -a 256 cmdref-${VERSION}-darwin-universal.tar.gz

```

```
file commandref
commandref: Mach-O universal binary with 2 architectures: [x86_64:Mach-O 64-bit executable x86_64] [arm64]
commandref (for architecture x86_64):	Mach-O 64-bit executable x86_64
commandref (for architecture arm64):	Mach-O 64-bit executable arm64
```

```
TO DO:

Shell autocomplete isn’t automatic for new commands. You must ship a completion script for zsh/bash/fish and install it via Homebrew.

Homebrew supports formulae that install completion files (zsh: share/zsh/site-functions, etc.)
```

```
sudo apt update
sudo apt install -y postgresql postgresql-contrib
```

```
4) Create DB + user
sudo -u postgres psql

CREATE DATABASE cmdref;
CREATE USER cmdref_user WITH ENCRYPTED PASSWORD 'STRONG_PASSWORD';
GRANT ALL PRIVILEGES ON DATABASE cmdref TO cmdref_user;
\q

```

```
go build -o commandref
lipo -create -output commandref commandref_amd64 commandref_arm64
```

```
PORT=8080
DATABASE_URL=postgresql://USER:PASSWORD@HOST/cmdref?sslmode=require
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
CMDREF_JWT_SECRET=some-long-random-string

```

```
systemd
 └─ commandref-api.service
     └─ /usr/local/bin/commandref-api
          ↳ listens on 127.0.0.1:8080
Nginx (443) → Go API
Neon Postgres (remote)

```

```
git clone YOUR_BACKEND_REPO
cd cmdref-backend
go build -o commandref-api
sudo mv commandref-api /usr/local/bin/

```

```
2) Create a dedicated system user (security best practice)
sudo useradd --system --no-create-home --shell /usr/sbin/nologin commandref
```

```
3) Create env file (secrets live here)
sudo mkdir -p /etc/commandref
sudo nano /etc/commandref/commandref.env

sudo chmod 600 /etc/commandref/commandref.env
sudo chown commandref:commandref /etc/commandref/commandref.env

```

```
sudo nano /etc/systemd/system/commandref-api.service


[Unit]
Description=CommandRef API
After=network.target

[Service]
User=commandref
Group=commandref

EnvironmentFile=/etc/commandref/commandref.env

ExecStart=/usr/local/bin/commandref-api
WorkingDirectory=/usr/local/bin

Restart=always
RestartSec=5

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true

[Install]
WantedBy=multi-user.target


```

```
sudo systemctl daemon-reexec
sudo systemctl daemon-reload
sudo systemctl enable commandref-api
sudo systemctl start commandref-api
```

```
Check status:

sudo systemctl status commandref-api


Logs:

journalctl -u commandref-api -f
```

```
Verify locally (before HTTPS)
curl http://127.0.0.1:8080/health
```

```
9) Updating the backend (no downtime)

When you push a new version:

sudo mv commandref-api /usr/local/bin/commandref-api
sudo chmod +x /usr/local/bin/commandref-api

sudo systemctl restart commandref-api
```

```
10) CLI production config

On your local machine:

export COMMANDREF_API_BASE=https://api.commandref.com
commandref login
```

```
3) Confirm systemd can read the file (permissions)

Run:

sudo -u commandref cat /etc/commandref/commandref.env
```

```
4) Reload and restart
sudo systemctl daemon-reload
sudo systemctl restart commandref-api
sudo systemctl status commandref-api


Follow logs:

journalctl -u commandref-api -f
```

```
4) Reload and restart
sudo systemctl daemon-reload
sudo systemctl restart commandref-api
sudo systemctl status commandref-api


Follow logs:

journalctl -u commandref-api -f
```

```
3) Quick log checks if domain fails
Nginx error log
sudo tail -n 100 /var/log/nginx/error.log

Nginx access log
sudo tail -n 100 /var/log/nginx/access.log

Backend logs
journalctl -u commandref-api -n 100 --no-pager
```

```
Check service status
sudo systemctl status commandref-api

View live service logs
journalctl -u commandref-api -f

Restart service
sudo systemctl restart commandref-api

Reload systemd configs
sudo systemctl daemon-reload

Show full service file
sudo systemctl cat commandref-api
```

```
Test Nginx config
sudo nginx -t

Reload Nginx
sudo systemctl reload nginx

Restart Nginx
sudo systemctl restart nginx

```

```
🔹 Firewall / Network (basic sanity)
Check open ports
ss -tulpn

Check listening ports
sudo lsof -i -P -n
```

```
systemctl list-timers | grep certbot
1️⃣ systemctl list-timers

This lists all systemd timers on the system.

Timers are systemd’s version of:

cron jobs

scheduled background tasks

Why this matters for your setup

Certbot renews SSL certificates automatically via a systemd timer.

Let’s Encrypt certificates:

expire every 90 days

are usually auto-renewed every 12 hours
```

```
7) Enable HTTPS with Certbot

Install certbot:

sudo apt install -y certbot python3-certbot-nginx


Issue certificate:

sudo certbot --nginx -d api.commandref.com


Verify from your laptop:

curl -i https://api.commandref.com/health

```

```
6) Install & configure Nginx

Install:

sudo apt update
sudo apt install -y nginx


Create site config:

sudo nano /etc/nginx/sites-available/commandref-api


Paste (replace domain):

server {
  listen 80;
  server_name api.commandref.com;

  location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
  }
}


Enable site:

sudo ln -s /etc/nginx/sites-available/commandref-api /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx


Test proxy locally:

curl http://127.0.0.1/health

```
