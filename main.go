// Command eos is a lightweight process manager for VPS environments.
// It manages background services using a self-contained Go binary with no
// Node.js runtime required. State is persisted in a local SQLite database,
// and a background daemon handles health monitoring and automatic restarts.
package main

import (
	"github.com/Elysium-Labs-EU/eos/cmd"
)

func main() {
	cmd.Execute()
}
