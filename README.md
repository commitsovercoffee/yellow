# 🟨 Yellow

Non-sticky notes for your terminal made with [Go](https://go.dev/) and [Bubbletea](https://github.com/charmbracelet/bubbletea).

For the longest time, I kept a simple `memo.md` file in my home directory to quickly jot down thoughts from meetings, ideas, or random notes.
These weren’t structured enough for my main note-taking app ([Obsidian](https://obsidian.md/)), and I wanted something quick and dirty I could just be done with, so I made this.

## Features

- ✨ Create, edit, and delete memos.
- 🔍 Filter and search through memos.
- ⌨️ Keyboard-driven interface.
- 💾 Persistent storage in JSON format.
- 🗑️ Deleted memos are wiped after 7 days.

## Installation

For Linux 64-bit:

```bash
curl -sL https://github.com/commitsovercoffee/yellow/releases/download/v1.0.0/yellow-linux-amd64 -o yellow && chmod +x yellow && sudo mv yellow /usr/local/bin/
```

## License

This project is open source and available under the AGPL-3.0 license.
