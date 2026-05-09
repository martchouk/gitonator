go build -o github.mcp .

export GITHUB_TOKEN=YOUR_PAT #GitHub personal access token or GitHub App token
export GITHUB_OWNER=YOUR_GITHUB_NAME #repository owner/org
export GITHUB_REPO=github.mcp #this repository name
export HTTP_ADDR='127.0.0.1:7777'
export WEBHOOK_SECRET='YOUR_WEBHOOK_SECRET'
export SQLITE_PATH=orchestrator.db
export DISPATCH_DIR=./dispatch
export DISPATCH_COMMAND=''
export DISPATCH_TMUX_TEMPLATE=''
export STAKEHOLDER_OVERRIDE=''
export AGENT_SHARED_TOKEN='YOUR_AGENT_SHARED_TOKEN'
export LOG_LEVEL=DEBUG #optional, set to DEBUG for stderr debug logs
export STALE_AFTER_SECONDS=900
export RECOVER_EVERY_SECONDS=30

#go run main.go
./github.mcp
