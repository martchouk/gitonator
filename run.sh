go build -o github.mcp .

export GITHUB_TOKEN=$YOUR_GITHUB_TOKEN #GitHub personal access token or GitHub App token
export GITHUB_OWNER=martchouk #repository owner/org
export GITHUB_REPO=github.mcp #repository name
export LOG_LEVEL=DEBUG #optional, set to DEBUG for stderr debug logs
export WEBHOOK_ADDR=:8099
export WEBHOOK_SECRET=your_webhook_secret
export SQLITE_PATH=orchestrator.db
export DISPATCH_DIR=./dispatch
export DISPATCH_COMMAND=''
export DISPATCH_TMUX_TEMPLATE=''
export STAKEHOLDER_OVERRIDE=''

#go run main.go
./github.mcp
