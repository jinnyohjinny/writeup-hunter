#!/bin/bash

# Configuration
REPO_DIR="/app"
BACKUP_INTERVAL=43200 # 12 hours in seconds
GIT_REMOTE="origin"
GIT_BRANCH="main"

configure_git() {
    # Only configure if variables exist
    if [[ -n "$GITHUB_USERNAME" && -n "$GITHUB_PAT" ]]; then
        git config --global url."https://${GITHUB_USERNAME}:${GITHUB_PAT}@github.com".insteadOf "https://github.com"
        git config --global user.email "writeup-hunter@docker"
        git config --global user.name "Writeup Hunter"
        echo "$(date) - Git configured with PAT authentication"
    else
        echo "$(date) - WARNING: GitHub credentials not set, Git operations may fail"
    fi
}
# Function to perform git backup
git_backup() {
    cd "$REPO_DIR" || exit 1

    if [ ! -f /root/.gitconfig ]; then
        configure_git
    fi
    
    # Check if there are changes to commit
    if [[ -n $(git status -s) ]]; then
        echo "$(date) - Changes detected, creating backup..."
        
        # Add all changes
        git add .
        
        # Commit with timestamp
        git commit -m "Auto-backup $(date '+%Y-%m-%d %H:%M:%S')"
        
        # Push to remote
        if git push "$GIT_REMOTE" "$GIT_BRANCH"; then
            echo "$(date) - Backup completed successfully"
        else
            echo "$(date) - ERROR: Failed to push backup"
            return 1
        fi
    else
        echo "$(date) - No changes detected, skipping backup"
    fi
}

# Main loop
while true; do
    echo "$(date) - Starting writeup-hunter..."
    cd "$REPO_DIR" || exit 1
    
    # Run the application
    if go run main.go; then
        echo "$(date) - writeup-hunter completed successfully"
    else
        echo "$(date) - ERROR: writeup-hunter failed"
    fi
    
    # Perform git backup
    if ! git_backup; then
        echo "$(date) - WARNING: Backup failed, continuing anyway"
    fi
    
    # Wait before next run
    echo "$(date) - Waiting $((BACKUP_INTERVAL/3600)) hours before next run..."
    sleep "$BACKUP_INTERVAL"
done